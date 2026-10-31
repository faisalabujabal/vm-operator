// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

func zoneGovernor(name string, zones []string, minimum, maximum, def string) *vmopv1.VirtualMachineClass {
	return &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tenant", Generation: 1}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &zones}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse(minimum)), Max: ptr.To(resource.MustParse(maximum)), Default: ptr.To(resource.MustParse(def))}}}}}
}

func TestValueBasedZoneSelection(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		zones       []string
		ambiguous   bool
	}{
		{"omitted", "", []string{"a", "b", "c"}, true},
		{"two", "2", []string{"a"}, false},
		{"six", "6", []string{"b", "c"}, false},
		{"ten", "10", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := pkgcfg.NewContextWithDefaultConfig()
			scheme := runtime.NewScheme()
			g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
			a := zoneGovernor("governor-a", []string{"a"}, "2", "4", "2")
			bc := zoneGovernor("governor-bc", []string{"b", "c"}, "5", "8", "5")
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, bc).Build()
			vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
			if tc.value != "" {
				vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse(tc.value))}}
			}
			before := vm.DeepCopy()
			result, err := governance.ResolveZones(ctx, client, vm, []string{"c", "a", "b", "a"})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Zones).To(Equal(tc.zones))
			if tc.ambiguous {
				g.Expect(result.AmbiguousFields).NotTo(BeEmpty())
			} else {
				g.Expect(result.AmbiguousFields).To(BeEmpty())
			}
			g.Expect(vm).To(Equal(before))
			switch {
			case tc.ambiguous:
				g.Expect(result.Error()).To(MatchError(ContainSubstring("resources.size.cpu")))
				g.Expect(result.Error().Error()).To(ContainSubstring("governor-bc"))
			case len(tc.zones) == 0:
				g.Expect(result.Error()).To(MatchError(ContainSubstring("no eligible zones")))
			default:
				g.Expect(result.Error()).NotTo(HaveOccurred())
			}
		})
	}
}

func TestZoneSelectionOverlapAndUncertainty(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	a := zoneGovernor("a", []string{"a"}, "2", "6", "5")
	b := zoneGovernor("b", []string{"b"}, "5", "8", "5")
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b).Build()
	vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
	evaluate := func() governance.ZoneSelection {
		r, err := governance.ResolveZones(ctx, client, vm, []string{"a", "b"})
		g.Expect(err).NotTo(HaveOccurred())
		return r
	}
	// Equal missing defaults are unambiguous, even with different governor names.
	g.Expect(evaluate().Error()).NotTo(HaveOccurred())
	vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse("5"))}}
	g.Expect(evaluate().Zones).To(Equal([]string{"a", "b"}))
	g.Expect(evaluate().Error()).NotTo(HaveOccurred())

	a.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{ExtraConfig: &vmopv1.ExtraConfigListPolicy{Denied: &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Fixed: ptr.To("denied")}}}}}
	g.Expect(client.Update(ctx, a)).To(Succeed())
	g.Expect(evaluate().Error()).NotTo(HaveOccurred()) // Omitted optional list is empty, not an unknown default.
	a.Spec.MemoryAdvanced = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{HotAddEnabled: &vmopv1.BoolPolicy{Default: ptr.To(true), AllowOverride: ptr.To(false)}}
	g.Expect(client.Update(ctx, a)).To(Succeed())
	g.Expect(evaluate().AmbiguousFields).To(ContainElement("memoryAdvanced.hotAddEnabled"))
	vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{HotAddEnabled: ptr.To(false)}
	g.Expect(evaluate().Zones).To(Equal([]string{"b"}))
	// Foreign namespace classes and deleting governors never affect selection.
	foreign := zoneGovernor("foreign", []string{"b"}, "1", "1", "1")
	foreign.Namespace = "other"
	g.Expect(client.Create(ctx, foreign)).To(Succeed())
	g.Expect(evaluate().Zones).To(Equal([]string{"b"}))
	conflict := zoneGovernor("conflict", []string{"b"}, "1", "8", "1")
	g.Expect(client.Create(ctx, conflict)).To(Succeed())
	_, err := governance.ResolveZones(ctx, client, vm, []string{"b"})
	g.Expect(err).To(HaveOccurred())
}

func TestZoneHardwareEvidence(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "selected", Namespace: "tenant", Generation: 3}, Status: vmopv1.VirtualMachineClassStatus{ObservedGeneration: 3, Zones: []vmopv1.VirtualMachineClassZoneStatus{{Name: "a", Conditions: []metav1.Condition{{Type: vmopv1.VirtualMachineClassConditionHardwareCompatible, ObservedGeneration: 3, Status: metav1.ConditionFalse, Reason: "HardwareUnsupported", Message: "requested device unavailable"}}}}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()
	vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}, Spec: vmopv1.VirtualMachineSpec{ClassName: "selected"}}
	evaluate := func() governance.ZoneSelection {
		r, err := governance.ResolveZones(ctx, client, vm, []string{"a", "b"})
		g.Expect(err).NotTo(HaveOccurred())
		return r
	}
	g.Expect(evaluate().Zones).To(Equal([]string{"b"}))
	g.Expect(client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
	class.Status.Zones[0].Conditions[0].ObservedGeneration = 2
	g.Expect(client.Update(ctx, class)).To(Succeed())
	g.Expect(evaluate().Zones).To(Equal([]string{"a", "b"}))
	class.Status.Zones[0].Conditions[0].ObservedGeneration = 3
	class.Status.Zones[0].Conditions[0].Status = metav1.ConditionUnknown
	g.Expect(client.Update(ctx, class)).To(Succeed())
	g.Expect(evaluate().Zones).To(HaveLen(2))
	class.Status.Zones[0].Conditions[0].Status = metav1.ConditionFalse
	g.Expect(client.Update(ctx, class)).To(Succeed())
	instance := &vmopv1.VirtualMachineClassInstance{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "tenant"}}
	instance.Spec.VirtualMachineClassSpec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("2"))
	g.Expect(client.Create(ctx, instance)).To(Succeed())
	vm.Spec.Class = &vmopv1common.LocalObjectRef{Name: "old"}
	g.Expect(evaluate().Zones).To(HaveLen(2))
}

func TestZoneSelectionEquivalentDefaults(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	a := zoneGovernor("a", []string{"a"}, "2", "6", "5")
	b := zoneGovernor("b", []string{"b"}, "5", "8", "5")
	a.Spec.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1Gi"))}
	b.Spec.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1073741824"))}
	a.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{PNUMANodeAffinity: &vmopv1.Int32SetPolicy{Default: &[]int32{0, 1}}}
	b.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{PNUMANodeAffinity: &vmopv1.Int32SetPolicy{Default: &[]int32{1, 0}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b).Build()
	vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
	result, err := governance.ResolveZones(ctx, client, vm, []string{"a", "b"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Error()).NotTo(HaveOccurred())
	g.Expect(result.Zones).To(Equal([]string{"a", "b"}))
	b.Spec.Resources.Size.Memory.Default = ptr.To(resource.MustParse("2Gi"))
	g.Expect(client.Update(ctx, b)).To(Succeed())
	result, err = governance.ResolveZones(ctx, client, vm, []string{"a", "b"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.AmbiguousFields).To(Equal([]string{"resources.size.memory"}))
}
