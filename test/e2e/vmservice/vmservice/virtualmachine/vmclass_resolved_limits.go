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
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
)

// VMClassResolvedLimitsSpec verifies effective limits against real discovery.
func VMClassResolvedLimitsSpec(ctx context.Context, inputGetter func() VMClassSchemaUpgradeInput) {
	It("resolves hardware choices within class and governing limits and refreshes them", Label("extended-functional", "experimental"), func() {
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
		var targets vimv1.ConfigTargetList
		var options vimv1.VirtualMachineConfigOptionsList
		Expect(admin.List(ctx, &zones, ctrlclient.InNamespace(input.WCPNamespaceName))).To(Succeed())
		Expect(admin.List(ctx, &targets)).To(Succeed())
		Expect(admin.List(ctx, &options)).To(Succeed())
		zone, cpus, memory := resolvedLimitsFixture(zones.Items, targets.Items, options.Items)
		if zone == nil {
			Skip("requires current cluster-specific CPU/memory discovery with at least three CPU choices")
		}

		// This namespace has no workloads or capacity. Its logical Zone refers to
		// one real cluster for discovery, without copying resource pools/folders.
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "vmclass-limits-"}}
		Expect(admin.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(admin.Delete(cleanupCtx, namespace)).To(Succeed()) })
		zone.Namespace = namespace.Name
		Expect(admin.Create(ctx, zone)).To(Succeed())
		ceiling := &vmopv1.VirtualMachineClass{
			ObjectMeta: metav1.ObjectMeta{Name: "ceiling", Namespace: namespace.Name},
			Spec: vmopv1.VirtualMachineClassSpec{
				Zones: &[]string{zone.Name},
				Governs: &vmopv1.VirtualMachineClassGovernance{
					ExistingVMs: "AllowOnViolation", Enforcement: "Deny",
				},
				Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{
					CPU:    &vmopv1.ResourceQuantityRange{Min: ptr.To(cpus[0]), Max: ptr.To(cpus[len(cpus)-1]), Default: ptr.To(cpus[0])},
					Memory: &vmopv1.ResourceQuantityRange{Min: memory.Min, Max: memory.Max, Default: memory.Min},
				}},
			},
		}
		class := ceiling.DeepCopy()
		class.Name = "selectable"
		class.Spec.Governs = nil
		class.Spec.Resources.Size.CPU.Max = ptr.To(cpus[len(cpus)-2])
		Expect(admin.Create(ctx, ceiling)).To(Succeed())
		Expect(admin.Create(ctx, class)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			expectResolvedResources(g, class, cpus[:len(cpus)-1], memory)
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("narrowing governance without rewriting the selectable class")
		Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(ceiling), ceiling)).To(Succeed())
		ceiling.Spec.Resources.Size.CPU.Max = ptr.To(cpus[0])
		Expect(admin.Update(ctx, ceiling)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			expectResolvedResources(g, class, cpus[:1], memory)
			g.Expect(class.Spec.Resources.Size.CPU.Max.Cmp(cpus[len(cpus)-2])).To(BeZero())
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("falling back to hardware choices when both policies omit bounds")
		Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(ceiling), ceiling)).To(Succeed())
		ceiling.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(cpus[0])}
		ceiling.Spec.Resources.Size.Memory = nil
		Expect(admin.Update(ctx, ceiling)).To(Succeed())
		Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
		class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(cpus[0])}
		class.Spec.Resources.Size.Memory = nil
		Expect(admin.Update(ctx, class)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			expectResolvedResources(g, class, cpus, memory)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})
}

// resolvedLimitsFixture chooses one authoritative source snapshot. It does not
// synthesize discovery, combine clusters, or expose pool totals in class status.
func resolvedLimitsFixture(zones []topologyv1.Zone, targets []vimv1.ConfigTarget, options []vimv1.VirtualMachineConfigOptions) (*topologyv1.Zone, []resource.Quantity, vmopv1.VirtualMachineClassResolvedQuantityRange) {
	for _, zone := range zones {
		if !zone.DeletionTimestamp.IsZero() {
			continue
		}
		for _, target := range targets {
			if !slices.Contains(zone.Spec.ManagedVMs.ClusterMoIDs, target.Spec.ID.ID) ||
				!target.DeletionTimestamp.IsZero() || target.Status.ObservedGeneration != target.Generation ||
				!apimeta.IsStatusConditionTrue(target.Status.Conditions, vimv1.ReadyConditionType) ||
				target.Status.MaxCPUsPerVM <= 0 || target.Status.SupportedMaxMem == nil {
				continue
			}
			for _, option := range options {
				if option.Spec.HardwareVersion != target.Status.MaxHardwareVersion ||
					option.Annotations["vmoperator.vmware.com/environment-browser-cluster"] != target.Spec.ID.ID ||
					!option.DeletionTimestamp.IsZero() || option.Status.ObservedGeneration != option.Generation ||
					!apimeta.IsStatusConditionTrue(option.Status.Conditions, vimv1.ReadyConditionType) ||
					!slices.ContainsFunc(option.OwnerReferences, func(ref metav1.OwnerReference) bool { return ref.UID != "" && ref.UID == target.UID }) {
					continue
				}
				hardware := option.Status.HardwareOptions
				if hardware == nil || hardware.Memory == nil {
					continue
				}
				var cpus []resource.Quantity
				for _, n := range hardware.NumCPU {
					if n > 0 && n <= target.Status.MaxCPUsPerVM {
						cpus = append(cpus, *resource.NewQuantity(int64(n), resource.DecimalSI))
					}
				}
				slices.SortFunc(cpus, func(a, b resource.Quantity) int { return a.Cmp(b) })
				cpus = slices.CompactFunc(cpus, func(a, b resource.Quantity) bool { return a.Cmp(b) == 0 })
				maximum := hardware.Memory.Max.DeepCopy()
				if maximum.Cmp(*target.Status.SupportedMaxMem) > 0 {
					maximum = target.Status.SupportedMaxMem.DeepCopy()
				}
				if len(cpus) < 3 || hardware.Memory.Min.Sign() <= 0 || hardware.Memory.Min.Cmp(maximum) > 0 {
					continue
				}
				return &topologyv1.Zone{
					ObjectMeta: metav1.ObjectMeta{Name: "limits-zone"},
					Spec:       topologyv1.ZoneSpec{Zone: zone.Spec.Zone, ManagedVMs: topologyv1.VSphereEntityInfo{ClusterMoIDs: []string{target.Spec.ID.ID}}},
				}, cpus, vmopv1.VirtualMachineClassResolvedQuantityRange{Min: ptr.To(hardware.Memory.Min.DeepCopy()), Max: &maximum}
			}
		}
	}
	return nil, nil, vmopv1.VirtualMachineClassResolvedQuantityRange{}
}

func expectResolvedResources(g Gomega, class *vmopv1.VirtualMachineClass, cpus []resource.Quantity, memory vmopv1.VirtualMachineClassResolvedQuantityRange) {
	g.Expect(class.Status.ObservedGeneration).To(Equal(class.Generation))
	g.Expect(class.Status.Zones).To(HaveLen(1))
	fields := class.Status.Zones[0].ResolvedLimits
	for _, path := range []string{"resources.size.cpu", "resources.size.memory"} {
		i := slices.IndexFunc(fields, func(f vmopv1.VirtualMachineClassResolvedField) bool { return f.Path == path })
		g.Expect(i).To(BeNumerically(">=", 0))
		field := fields[i]
		g.Expect(field.State).To(Equal("Complete"), field.Reason)
		g.Expect(field.Available).NotTo(BeNil())
		g.Expect(field.Available.Quantity).NotTo(BeNil())
		if path == "resources.size.cpu" {
			g.Expect(field.Available.Quantity.Values).To(HaveLen(len(cpus)))
			for j := range cpus {
				g.Expect(field.Available.Quantity.Values[j].Cmp(cpus[j])).To(BeZero())
			}
		} else {
			g.Expect(field.Available.Quantity.Ranges).To(HaveLen(1))
			g.Expect(field.Available.Quantity.Ranges[0].Min).NotTo(BeNil())
			g.Expect(field.Available.Quantity.Ranges[0].Max).NotTo(BeNil())
			g.Expect(field.Available.Quantity.Ranges[0].Min.Cmp(*memory.Min)).To(BeZero())
			g.Expect(field.Available.Quantity.Ranges[0].Max.Cmp(*memory.Max)).To(BeZero())
		}
	}
}
