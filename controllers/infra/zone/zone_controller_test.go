// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package zone_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/controllers/infra/zone"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	providerfake "github.com/vmware-tanzu/vm-operator/pkg/providers/fake"
	"github.com/vmware-tanzu/vm-operator/pkg/util/kube/cource"
	vsclient "github.com/vmware-tanzu/vm-operator/pkg/util/vsphere/client"
	"github.com/vmware-tanzu/vm-operator/pkg/util/vsphere/watcher"
	vmwatcher "github.com/vmware-tanzu/vm-operator/services/vm-watcher"
	"github.com/vmware-tanzu/vm-operator/test/builder"
	"github.com/vmware-tanzu/vm-operator/test/testutil"
	"github.com/vmware/govmomi/object"
	vimtypes "github.com/vmware/govmomi/vim25/types"
)

var _ = Describe(
	"Reconcile",
	Label(
		testlabels.Controller,
		testlabels.EnvTest,
		testlabels.API,
	),
	func() {
		var (
			ctx       context.Context
			vcSimCtx  *builder.IntegrationTestContextForVCSim
			provider  *providerfake.VMProvider
			initEnvFn builder.InitVCSimEnvFn
		)

		BeforeEach(func() {
			ctx = context.Background()
			ctx = logr.NewContext(ctx, testutil.GinkgoLogr(4))
		})

		JustBeforeEach(func() {
			ctx = pkgcfg.WithContext(ctx, pkgcfg.Default())
			ctx = pkgcfg.UpdateContext(
				ctx,
				func(config *pkgcfg.Config) {
					config.Features.WorkloadDomainIsolation = true
				},
			)
			ctx = cource.WithContext(ctx)
			ctx = watcher.WithContext(ctx)

			provider = providerfake.NewVMProvider()
			provider.VSphereClientFn = func(ctx context.Context) (*vsclient.Client, error) {
				return vsclient.NewClient(ctx, vcSimCtx.VCClientConfig)
			}

			vcSimCtx = builder.NewIntegrationTestContextForVCSim(
				ctx,
				builder.VCSimTestConfig{},
				func(ctx *pkgctx.ControllerManagerContext, mgr ctrlmgr.Manager) error {
					if err := vmwatcher.AddToManager(ctx, mgr); err != nil {
						return err
					}
					return zone.AddToManager(ctx, mgr)
				},
				func(ctx *pkgctx.ControllerManagerContext, _ ctrlmgr.Manager) error {
					ctx.VMProvider = provider
					return nil
				},
				initEnvFn)
			Expect(vcSimCtx).ToNot(BeNil())

			vcSimCtx.BeforeEach()

			ctx = vcSimCtx
		})

		AfterEach(func() {
			vcSimCtx.AfterEach()
			vcSimCtx = nil
		})

		When("new zones are added", func() {
			var (
				nsInfo builder.WorkloadNamespaceInfo
			)

			JustBeforeEach(func() {
				nsInfo = vcSimCtx.CreateWorkloadNamespace()

				By("ensure all zones have finalizers", func() {
					Eventually(func(g Gomega) {
						var obj topologyv1.ZoneList
						g.Expect(vcSimCtx.Client.List(ctx, &obj, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
						g.Expect(obj.Items).To(HaveLen(vcSimCtx.ZoneCount))
						g.Expect(obj.Items).ToNot(BeEmpty())

						for i := range obj.Items {
							g.Expect(obj.Items[i].Finalizers).To(ConsistOf([]string{zone.Finalizer}))
						}
					}).Should(Succeed())
				})
			})

			When("no vms exist in the zone's vm service folder", func() {
				const (
					vmName = "my-vm-1"
				)

				var (
					vm *object.VirtualMachine
				)

				BeforeEach(func() {
					initEnvFn = func(ctx *builder.IntegrationTestContextForVCSim) {
						vmList, err := ctx.Finder.VirtualMachineList(ctx, "*")
						Expect(err).ToNot(HaveOccurred())
						Expect(vmList).ToNot(BeEmpty())
						vm = vmList[0]

						dcFolders, err := ctx.Datacenter.Folders(ctx)
						Expect(err).ToNot(HaveOccurred())
						Expect(dcFolders).ToNot(BeNil())
						Expect(dcFolders.VmFolder).ToNot(BeNil())

						By("creating vm in k8s", func() {
							obj := builder.DummyBasicVirtualMachine(
								vmName,
								ctx.NSInfo.Namespace)
							Expect(ctx.Client.Create(ctx, obj)).To(Succeed())
						})

						By("moving vm into the root folder", func() {
							t, err := dcFolders.VmFolder.MoveInto(
								ctx,
								[]vimtypes.ManagedObjectReference{vm.Reference()})
							Expect(err).ToNot(HaveOccurred())
							Expect(t).ToNot(BeNil())
							Expect(t.Wait(ctx)).To(Succeed())
						})

						By("adding namespacedName to vm's extraConfig", func() {
							t, err := vm.Reconfigure(ctx, vimtypes.VirtualMachineConfigSpec{
								ExtraConfig: []vimtypes.BaseOptionValue{
									&vimtypes.OptionValue{
										Key:   "vmservice.namespacedName",
										Value: ctx.NSInfo.Namespace + "/" + vmName,
									},
								},
							})
							Expect(err).ToNot(HaveOccurred())
							Expect(t).ToNot(BeNil())
							Expect(t.Wait(ctx)).To(Succeed())
						})
					}
				})

				Specify("no reconcile requests should be enqueued for vms", func() {
					chanSource := cource.FromContext(ctx, "VirtualMachine")
					Consistently(chanSource).ShouldNot(Receive())
				})

				When("the vm is relocated into the zone's folder", func() {
					JustBeforeEach(func() {
						t, err := nsInfo.Folder.MoveInto(
							ctx,
							[]vimtypes.ManagedObjectReference{vm.Reference()})
						Expect(err).ToNot(HaveOccurred())
						Expect(t).ToNot(BeNil())
						Expect(t.Wait(ctx)).To(Succeed())
					})
					Specify("a reconcile request should be enqueued for vm", func() {
						chanSource := cource.FromContext(ctx, "VirtualMachine")

						var e event.GenericEvent
						Eventually(chanSource).Should(Receive(&e))
						Expect(e.Object.GetNamespace()).To(Equal(vcSimCtx.NSInfo.Namespace))
						Expect(e.Object.GetName()).To(Equal(vmName))
					})

					When("a single zone with the vm's folder is removed", func() {
						JustBeforeEach(func() {
							var list topologyv1.ZoneList
							Expect(vcSimCtx.Client.List(
								ctx,
								&list,
								ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())

							for i := range list.Items {
								z := &list.Items[i]
								if z.Spec.ManagedVMs.FolderMoID == nsInfo.Folder.Reference().Value {
									Expect(vcSimCtx.Client.Delete(ctx, z)).To(Succeed())
									Eventually(func(g Gomega) {
										key := ctrlclient.ObjectKeyFromObject(z)
										g.Expect(vcSimCtx.Client.Get(vcSimCtx, key, z)).ToNot(Succeed())
									}).Should(Succeed())

									break
								}
							}
						})

						Specify("a change to the vm should cause it to be reconciled", func() {
							// Pull any events off of the source channel for 10
							// seconds. This should give the zone enough time to be
							// removed.
							chanSource := cource.FromContext(ctx, "VirtualMachine")

							Eventually(func(g Gomega) {
								g.Expect(chanSource).ToNot(Receive())
							}, time.Second*10).Should(Succeed())

							t, err := vm.Reconfigure(ctx, vimtypes.VirtualMachineConfigSpec{
								ExtraConfig: []vimtypes.BaseOptionValue{
									&vimtypes.OptionValue{
										Key:   "guestinfo.ipaddr",
										Value: "1.2.3.4",
									},
								},
							})
							Expect(err).ToNot(HaveOccurred())
							Expect(t).ToNot(BeNil())
							Expect(t.Wait(ctx)).To(Succeed())

							var e event.GenericEvent
							Eventually(chanSource).Should(Receive(&e))
							Expect(e.Object.GetNamespace()).To(Equal(vcSimCtx.NSInfo.Namespace))
							Expect(e.Object.GetName()).To(Equal(vmName))
						})
					})

					When("all zones with the vm's folder are removed", func() {
						JustBeforeEach(func() {
							var list topologyv1.ZoneList
							Expect(vcSimCtx.Client.List(
								ctx,
								&list,
								ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())

							for i := range list.Items {
								z := &list.Items[i]
								if z.Spec.ManagedVMs.FolderMoID == nsInfo.Folder.Reference().Value {
									Expect(vcSimCtx.Client.Delete(ctx, z)).To(Succeed())
									Eventually(func(g Gomega) {
										key := ctrlclient.ObjectKeyFromObject(z)
										g.Expect(vcSimCtx.Client.Get(vcSimCtx, key, z)).ToNot(Succeed())
									}).Should(Succeed())
								}
							}
						})

						Specify("a change to the vm should not cause it to be reconciled", func() {
							// Pull any events off of the source channel for 10
							// seconds. This should give the zones enough time to be
							// removed.
							chanSource := cource.FromContext(ctx, "VirtualMachine")

							Eventually(func(g Gomega) {
								g.Expect(chanSource).ToNot(Receive())
							}, time.Second*10).Should(Succeed())

							t, err := vm.Reconfigure(ctx, vimtypes.VirtualMachineConfigSpec{
								ExtraConfig: []vimtypes.BaseOptionValue{
									&vimtypes.OptionValue{
										Key:   "guestinfo.ipaddr",
										Value: "1.2.3.4",
									},
								},
							})
							Expect(err).ToNot(HaveOccurred())
							Expect(t).ToNot(BeNil())
							Expect(t.Wait(ctx)).To(Succeed())

							Consistently(chanSource).ShouldNot(Receive())
						})
					})
				})
			})
		})
	})

var _ = Describe(
	"Reconcile ConfigTarget fan-out",
	Label(
		testlabels.Controller,
		testlabels.EnvTest,
		testlabels.API,
	),
	func() {
		var (
			ctx      context.Context
			vcSimCtx *builder.IntegrationTestContextForVCSim
			provider *providerfake.VMProvider
		)

		BeforeEach(func() {
			ctx = context.Background()
			ctx = logr.NewContext(ctx, testutil.GinkgoLogr(4))
		})

		JustBeforeEach(func() {
			ctx = pkgcfg.WithContext(ctx, pkgcfg.Default())
			ctx = pkgcfg.UpdateContext(ctx, func(config *pkgcfg.Config) {
				config.Features.WorkloadDomainIsolation = true
				config.Features.VMServiceClasslessVM = true
			})
			ctx = cource.WithContext(ctx)
			ctx = watcher.WithContext(ctx)

			provider = providerfake.NewVMProvider()
			provider.VSphereClientFn = func(ctx context.Context) (*vsclient.Client, error) {
				return vsclient.NewClient(ctx, vcSimCtx.VCClientConfig)
			}

			vcSimCtx = builder.NewIntegrationTestContextForVCSim(
				ctx,
				builder.VCSimTestConfig{},
				func(ctx *pkgctx.ControllerManagerContext, mgr ctrlmgr.Manager) error {
					if err := vmwatcher.AddToManager(ctx, mgr); err != nil {
						return err
					}
					return zone.AddToManager(ctx, mgr)
				},
				func(ctx *pkgctx.ControllerManagerContext, _ ctrlmgr.Manager) error {
					ctx.VMProvider = provider

					return nil
				},
				nil)
			Expect(vcSimCtx).ToNot(BeNil())

			vcSimCtx.BeforeEach()

			ctx = vcSimCtx
		})

		AfterEach(func() {
			vcSimCtx.AfterEach()
			vcSimCtx = nil
		})

		When("zones have pool MoIDs", func() {
			var (
				nsInfo builder.WorkloadNamespaceInfo
			)

			JustBeforeEach(func() {
				nsInfo = vcSimCtx.CreateWorkloadNamespace()

				By("wait for finalizers", func() {
					Eventually(func(g Gomega) {
						var list topologyv1.ZoneList
						g.Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
						g.Expect(list.Items).To(HaveLen(vcSimCtx.ZoneCount))

						for i := range list.Items {
							g.Expect(list.Items[i].Finalizers).To(ContainElement(zone.Finalizer))
						}
					}).WithTimeout(30 * time.Second).Should(Succeed())
				})
			})

			It("updates the governance mirror on class changes and deletion", func() {
				class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "governor", Namespace: nsInfo.Namespace}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{ExistingVMs: "AllowOnViolation"}}}
				Expect(vcSimCtx.Client.Create(ctx, class)).To(Succeed())
				Eventually(func(g Gomega) {
					var zones topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &zones, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					g.Expect(zones.Items).NotTo(BeEmpty())
					for _, z := range zones.Items {
						g.Expect(z.Status.GoverningClasses).To(Equal([]string{class.Name}))
					}
				}).Should(Succeed())
				class.Spec.Governs.Zones = &[]string{}
				Expect(vcSimCtx.Client.Update(ctx, class)).To(Succeed())
				Eventually(func(g Gomega) {
					var zones topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &zones, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					for _, z := range zones.Items {
						g.Expect(z.Status.GoverningClasses).To(BeEmpty())
					}
				}).Should(Succeed())
				class.Spec.Governs.Zones = nil
				Expect(vcSimCtx.Client.Update(ctx, class)).To(Succeed())
				Eventually(func(g Gomega) {
					var zones topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &zones, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					for _, z := range zones.Items {
						g.Expect(z.Status.GoverningClasses).To(Equal([]string{class.Name}))
					}
				}).Should(Succeed())
				Expect(vcSimCtx.Client.Delete(ctx, class)).To(Succeed())
				Eventually(func(g Gomega) {
					var zones topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &zones, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					for _, z := range zones.Items {
						g.Expect(z.Status.GoverningClasses).To(BeEmpty())
					}
				}).Should(Succeed())
			})

			It("creates one ConfigTarget per cluster", func() {
				Eventually(func(g Gomega) {
					var list topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					g.Expect(list.Items).ToNot(BeEmpty())

					for i := range list.Items {
						z := &list.Items[i]

						ccrs := vcSimCtx.GetAZClusterComputes(z.Name)
						g.Expect(ccrs).ToNot(BeEmpty())

						for _, ccr := range ccrs {
							clusterMoID := ccr.Reference().Value

							var ct vimv1.ConfigTarget
							g.Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct)).To(Succeed())
							g.Expect(ct.Spec.ID.ID).To(Equal(clusterMoID))
						}
					}
				}).WithTimeout(30 * time.Second).Should(Succeed())
			})

			It("is idempotent: ConfigTarget UID is stable across reconciles", func() {
				var clusterMoID string

				Eventually(func(g Gomega) {
					var list topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					g.Expect(list.Items).ToNot(BeEmpty())

					ccrs := vcSimCtx.GetAZClusterComputes(list.Items[0].Name)
					g.Expect(ccrs).ToNot(BeEmpty())
					clusterMoID = ccrs[0].Reference().Value

					var ct vimv1.ConfigTarget
					g.Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct)).To(Succeed())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				var originalUID types.UID

				var ct vimv1.ConfigTarget
				Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct)).To(Succeed())
				originalUID = ct.UID

				// Patch a label to force a re-reconcile of the zone.
				var list topologyv1.ZoneList
				Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
				z := &list.Items[0]
				p := ctrlclient.MergeFrom(z.DeepCopy())

				if z.Labels == nil {
					z.Labels = map[string]string{}
				}

				z.Labels["test-trigger"] = "1"
				Expect(vcSimCtx.Client.Patch(ctx, z, p)).To(Succeed())

				Consistently(func(g Gomega) {
					var ct2 vimv1.ConfigTarget
					g.Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct2)).To(Succeed())
					g.Expect(ct2.UID).To(Equal(originalUID))
				}, "5s", "1s").Should(Succeed())
			})

			It("does not delete a ConfigTarget when pool MoIDs are removed from the zone", func() {
				var clusterMoID string

				Eventually(func(g Gomega) {
					var list topologyv1.ZoneList
					g.Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
					g.Expect(list.Items).ToNot(BeEmpty())

					ccrs := vcSimCtx.GetAZClusterComputes(list.Items[0].Name)
					g.Expect(ccrs).ToNot(BeEmpty())
					clusterMoID = ccrs[0].Reference().Value

					var ct vimv1.ConfigTarget
					g.Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct)).To(Succeed())
				}).WithTimeout(30 * time.Second).Should(Succeed())

				// Remove all pool MoIDs from the zone so the controller derives
				// no cluster MoIDs on the next reconcile.
				var list topologyv1.ZoneList
				Expect(vcSimCtx.Client.List(ctx, &list, ctrlclient.InNamespace(nsInfo.Namespace))).To(Succeed())
				z := &list.Items[0]
				p := ctrlclient.MergeFrom(z.DeepCopy())
				z.Spec.ManagedVMs.PoolMoIDs = nil
				Expect(vcSimCtx.Client.Patch(ctx, z, p)).To(Succeed())

				Consistently(func(g Gomega) {
					var ct vimv1.ConfigTarget
					g.Expect(vcSimCtx.Client.Get(ctx, ctrlclient.ObjectKey{Name: clusterMoID}, &ct)).To(Succeed())
				}, "5s", "1s").Should(Succeed())
			})
		})
	})

var _ = Describe("Zone governance mirror", Label(testlabels.Controller), func() {
	It("uses optimistic locking, retries conflicts, skips unchanged writes and honors the live gate", func() {
		ctx := watcher.WithContext(pkgcfg.NewContextWithDefaultConfig())
		scheme := runtime.NewScheme()
		Expect(topologyv1.AddToScheme(scheme)).To(Succeed())
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		Expect(vimv1.AddToScheme(scheme)).To(Succeed())
		obj := &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "tenant", Finalizers: []string{zone.Finalizer}}}
		first := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "z", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{}}}
		second := first.DeepCopy()
		second.Name = "b"
		foreign := first.DeepCopy()
		foreign.Namespace = "other"
		writes, injectConflict := 0, true
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&topologyv1.Zone{}).WithObjects(obj, first, second, foreign).WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c ctrlclient.Client, subresource string, object ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.SubResourcePatchOption) error {
				writes++
				data, err := patch.Data(object)
				Expect(err).NotTo(HaveOccurred())
				var document map[string]any
				Expect(json.Unmarshal(data, &document)).To(Succeed())
				Expect(document["metadata"].(map[string]any)["resourceVersion"]).NotTo(BeEmpty())
				if injectConflict {
					injectConflict = false
					var current topologyv1.Zone
					Expect(c.Get(ctx, ctrlclient.ObjectKeyFromObject(object), &current)).To(Succeed())
					current.Status.Conditions = []metav1.Condition{{Type: "ProviderReady", Status: metav1.ConditionTrue, Reason: "Ready"}}
					Expect(c.Status().Update(ctx, &current)).To(Succeed())
				}
				return c.SubResource(subresource).Patch(ctx, object, patch, opts...)
			},
		}).Build()
		r := zone.NewReconciler(ctx, c, logr.Discard(), nil)
		request := ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(obj)}
		_, err := r.Reconcile(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		Expect(writes).To(BeZero())
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = true })
		_, err = r.Reconcile(context.Background(), request)
		Expect(apierrors.IsConflict(err)).To(BeTrue())
		Expect(c.Get(ctx, request.NamespacedName, obj)).To(Succeed())
		Expect(obj.Status.GoverningClasses).To(BeEmpty())
		_, err = r.Reconcile(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, obj)).To(Succeed())
		Expect(obj.Status.GoverningClasses).To(Equal([]string{"b", "z"}))
		Expect(obj.Status.Conditions).To(HaveLen(1))
		Expect(writes).To(Equal(2))
		_, err = r.Reconcile(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		Expect(writes).To(Equal(2))
		first.Spec.Governs.Zones = &[]string{}
		Expect(c.Update(ctx, first)).To(Succeed())
		Expect(c.Delete(ctx, second)).To(Succeed())
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = false })
		_, err = r.Reconcile(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		Expect(writes).To(Equal(2))
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = true })
		_, err = r.Reconcile(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, obj)).To(Succeed())
		Expect(obj.Status.GoverningClasses).To(BeEmpty())
		Expect(obj.Status.Conditions[0].Type).To(Equal("ProviderReady"))
		Expect(writes).To(Equal(3))
	})
})
