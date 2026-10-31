// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
)

// VMClassHardwareSpec requires a namespace with two zones whose vGPU profile
// inventories differ. It reads real discovery data and never edits inventory.
func VMClassHardwareSpec(ctx context.Context, inputGetter func() VMClassSchemaUpgradeInput) {
	It("accepts a zone-specific vGPU class and reports compatibility per zone", Label("extended-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		adminProxy, err := proxy.NewAdminClusterProxy(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { adminProxy.Dispose(ctx) })
		admin, err := adminProxy.GetAdminClient()
		Expect(err).NotTo(HaveOccurred())
		var zones topologyv1.ZoneList
		Expect(admin.List(ctx, &zones, ctrlclient.InNamespace(input.WCPNamespaceName))).To(Succeed())
		if len(zones.Items) < 2 {
			Skip("requires two namespace zones with different vGPU profile inventories")
		}
		var targets vimv1.ConfigTargetList
		Eventually(func(g Gomega) {
			g.Expect(admin.List(ctx, &targets)).To(Succeed())
			for _, z := range zones.Items {
				g.Expect(z.Spec.ManagedVMs.ClusterMoIDs).NotTo(BeEmpty())
				for _, id := range z.Spec.ManagedVMs.ClusterMoIDs {
					i := slices.IndexFunc(targets.Items, func(t vimv1.ConfigTarget) bool { return t.Spec.ID.ID == id })
					g.Expect(i).To(BeNumerically(">=", 0))
					g.Expect(targets.Items[i].Status.ObservedGeneration).To(Equal(targets.Items[i].Generation))
					g.Expect(apimeta.IsStatusConditionTrue(targets.Items[i].Status.Conditions, vimv1.ReadyConditionType)).To(BeTrue())
				}
			}
		}, 2*time.Minute, time.Second).Should(Succeed())
		profiles := map[string]map[string]bool{}
		for _, zone := range zones.Items {
			profiles[zone.Name] = map[string]bool{}
			for _, target := range targets.Items {
				if !slices.Contains(zone.Spec.ManagedVMs.ClusterMoIDs, target.Spec.ID.ID) {
					continue
				}
				for _, p := range target.Status.VGPUProfile {
					profiles[zone.Name][p.ProfileName] = true
				}
				for _, p := range target.Status.SharedGPUPassthroughTypes {
					profiles[zone.Name][p.VGPU] = true
				}
			}
		}
		var selected, supportedZone, unsupportedZone string
		for zone, available := range profiles {
			for profile := range available {
				for other, otherProfiles := range profiles {
					if !otherProfiles[profile] {
						selected, supportedZone, unsupportedZone = profile, zone, other
						break
					}
				}
				if selected != "" {
					break
				}
			}
			if selected != "" {
				break
			}
		}
		if selected == "" {
			Skip("no vGPU profile is present in only a subset of namespace zones")
		}
		class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "hardware-compatibility-", Namespace: input.WCPNamespaceName}, Spec: vmopv1.VirtualMachineClassSpec{Zones: &[]string{supportedZone, unsupportedZone}, Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{VGPUDevices: &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: selected}}}}}}}
		Expect(admin.Create(ctx, class)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(admin.Delete(cleanupCtx, class)).To(Succeed()) })
		Eventually(func(g Gomega) {
			g.Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			g.Expect(class.Status.Zones).To(HaveLen(2))
			for _, zone := range class.Status.Zones {
				condition := apimeta.FindStatusCondition(zone.Conditions, vmopv1.VirtualMachineClassConditionHardwareCompatible)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.ObservedGeneration).To(Equal(class.Generation))
				expected := metav1.ConditionFalse
				if zone.Name == supportedZone {
					expected = metav1.ConditionTrue
				}
				g.Expect(condition.Status).To(Equal(expected), condition.Message)
			}
		}, 2*time.Minute, time.Second).Should(Succeed())
	})
}
