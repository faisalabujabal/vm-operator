// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachineclass_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

var _ = Describe("Class hardware watches", Label(testlabels.Controller, testlabels.EnvTest, testlabels.API), func() {
	It("accepts incompatible classes and refreshes status on discovery and zone changes", func() {
		ctx := suite.NewIntegrationTestContext()
		DeferCleanup(ctx.AfterEach)
		old := pkgcfg.FromContext(suite).Features
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true; c.Features.ImmutableClasses = false })
		DeferCleanup(func() { pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features = old }) })
		target := &vimv1.ConfigTarget{ObjectMeta: metav1.ObjectMeta{GenerateName: "hardware-"}, Spec: vimv1.ConfigTargetSpec{ID: vimv1.ManagedObjectID{ID: ctx.Namespace}}}
		Expect(ctx.Client.Create(ctx, target)).To(Succeed())
		DeferCleanup(func() { Expect(ctx.Client.Delete(ctx, target)).To(Succeed()) })
		target.Status = vimv1.ConfigTargetStatus{ObservedGeneration: target.Generation, MaxCPUsPerVM: 8, SupportedMaxMem: ptr.To(resource.MustParse("8Gi")), MaxHardwareVersion: "vmx-21", Conditions: []metav1.Condition{{Type: vimv1.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Test", LastTransitionTime: metav1.Now()}}}
		Expect(ctx.Client.Status().Update(ctx, target)).To(Succeed())
		options := &vimv1.VirtualMachineConfigOptions{ObjectMeta: metav1.ObjectMeta{GenerateName: "hardware-options-", Annotations: map[string]string{"vmoperator.vmware.com/environment-browser-cluster": ctx.Namespace}, OwnerReferences: []metav1.OwnerReference{{APIVersion: vimv1.GroupVersion.String(), Kind: "ConfigTarget", Name: target.Name, UID: target.UID}}}, Spec: vimv1.VirtualMachineConfigOptionsSpec{HardwareVersion: "vmx-21"}}
		Expect(ctx.Client.Create(ctx, options)).To(Succeed())
		DeferCleanup(func() { Expect(ctx.Client.Delete(ctx, options)).To(Succeed()) })
		options.Status = vimv1.VirtualMachineConfigOptionsStatus{ObservedGeneration: options.Generation, Conditions: target.Status.Conditions, HardwareOptions: &vimv1.VirtualHardwareOption{HardwareVersion: "vmx-21", NumCPU: []int32{1, 2, 4, 8}, Memory: &vimv1.ResourceQuantityOption{ResourceQuantityRange: vimv1.ResourceQuantityRange{Min: resource.MustParse("128Mi"), Max: resource.MustParse("8Gi")}}}, Capabilities: &vimv1.VirtualMachineConfigCapability{NestedHVSupported: true}}
		Expect(ctx.Client.Status().Update(ctx, options)).To(Succeed())
		zone := &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: "hardware-zone", Namespace: ctx.Namespace}, Spec: topologyv1.ZoneSpec{Zone: topologyv1.AvailabilityZoneReference{APIVersion: topologyv1.GroupVersion.String(), Name: "hardware-zone"}, ManagedVMs: topologyv1.VSphereEntityInfo{ClusterMoIDs: []string{ctx.Namespace}}}}
		Expect(ctx.Client.Create(ctx, zone)).To(Succeed())
		class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "hardware-class-", Namespace: ctx.Namespace}, Spec: vmopv1.VirtualMachineClassSpec{CPUAdvanced: &vmopv1.VirtualMachineClassCPUAdvancedSpec{NestedHardwareVirtualizationEnabled: &vmopv1.BoolPolicy{Default: ptr.To(true)}}, Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{VGPUDevices: &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: "requested"}}}}}}}
		Expect(ctx.Client.Create(ctx, class)).To(Succeed())
		expectStatus := func(status metav1.ConditionStatus) {
			Eventually(func(g Gomega) {
				g.Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
				g.Expect(class.Status.Zones).To(HaveLen(1))
				condition := apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionHardwareCompatible)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(status))
				g.Expect(condition.ObservedGeneration).To(Equal(class.Generation))
				g.Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance)).NotTo(BeNil())
			}).Should(Succeed())
		}
		expectLimits := func(state string, values ...string) {
			Eventually(func(g Gomega) {
				g.Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
				g.Expect(class.Status.Zones).To(HaveLen(1))
				var cpu *vmopv1.VirtualMachineClassResolvedField
				for i := range class.Status.Zones[0].ResolvedLimits {
					entry := &class.Status.Zones[0].ResolvedLimits[i]
					if entry.Path == "resources.size.cpu" {
						cpu = entry
					}
				}
				g.Expect(cpu).NotTo(BeNil())
				g.Expect(cpu.State).To(Equal(state))
				if state == "Unknown" {
					g.Expect(cpu.Available).To(BeNil())
					return
				}
				g.Expect(cpu.Available).NotTo(BeNil())
				g.Expect(cpu.Available.Quantity).NotTo(BeNil())
				var actual []string
				for _, value := range cpu.Available.Quantity.Values {
					actual = append(actual, value.String())
				}
				g.Expect(actual).To(Equal(values))
			}).Should(Succeed())
		}
		expectStatus(metav1.ConditionFalse)
		expectLimits("Complete", "1", "2", "4", "8")
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(options), options)).To(Succeed())
		options.Status.HardwareOptions.NumCPU = []int32{1, 2, 4}
		Expect(ctx.Client.Status().Update(ctx, options)).To(Succeed())
		expectLimits("Complete", "1", "2", "4")
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(target), target)).To(Succeed())
		target.Status.SharedGPUPassthroughTypes = []vimv1.VirtualMachinePciSharedGpuPassthroughInfo{{VirtualMachineTargetInfo: vimv1.VirtualMachineTargetInfo{Name: "requested"}, VGPU: "requested"}}
		Expect(ctx.Client.Status().Update(ctx, target)).To(Succeed())
		expectStatus(metav1.ConditionTrue)
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(options), options)).To(Succeed())
		options.Status.Conditions[0].Status = metav1.ConditionFalse
		Expect(ctx.Client.Status().Update(ctx, options)).To(Succeed())
		expectStatus(metav1.ConditionUnknown)
		expectLimits("Unknown")
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(options), options)).To(Succeed())
		options.Status.Conditions[0].Status = metav1.ConditionTrue
		Expect(ctx.Client.Status().Update(ctx, options)).To(Succeed())
		expectStatus(metav1.ConditionTrue)
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(zone), zone)).To(Succeed())
		zone.Spec.ManagedVMs.ClusterMoIDs = []string{"undiscovered"}
		Expect(ctx.Client.Update(ctx, zone)).To(Succeed())
		expectStatus(metav1.ConditionUnknown)
		expectLimits("Unknown")
		Expect(ctx.Client.Delete(ctx, zone)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			g.Expect(class.Status.Zones).To(BeEmpty())
		}).Should(Succeed())
	})
})
