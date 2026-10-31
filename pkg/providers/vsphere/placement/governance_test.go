// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package placement

import (
	vimtypes "github.com/vmware/govmomi/vim25/types"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

func TestGovernancePlacementPools(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	a := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &[]string{"a"}}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("2")), Max: ptr.To(resource.MustParse("4")), Default: ptr.To(resource.MustParse("2"))}}}}}
	bc := a.DeepCopy()
	bc.Name = "bc"
	bc.Spec.Governs.Zones = &[]string{"b", "c"}
	bc.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("5")), Max: ptr.To(resource.MustParse("8")), Default: ptr.To(resource.MustParse("5"))}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, bc).Build()
	vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}, Spec: vmopv1.VirtualMachineSpec{Resources: &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse("6"))}}}}
	candidates := map[string][]string{"a": {"rp-a"}, "b": {"rp-b"}, "c": {"rp-c"}}
	noFrequency := func(string) (int64, error) { return 0, nil }
	allowed, err := filterGovernancePools(ctx, client, vm, candidates, noFrequency)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allowed).To(Equal(map[string][]string{"b": {"rp-b"}, "c": {"rp-c"}}))
	g.Expect(candidates).To(HaveLen(3))
	vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse("2"))
	// Shared group placement intersects each member's eligible pools.
	_, err = filterGovernancePools(ctx, client, vm, allowed, noFrequency)
	g.Expect(err).To(MatchError(ContainSubstring("no zone satisfies")))
	allowed, err = filterGovernancePools(ctx, client, vm, candidates, noFrequency)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allowed).To(Equal(map[string][]string{"a": {"rp-a"}}))
	vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse("10"))
	_, err = filterGovernancePools(ctx, client, vm, candidates, noFrequency)
	g.Expect(err).To(HaveOccurred())
	vm.Spec.Resources = nil
	_, err = filterGovernancePools(ctx, client, vm, candidates, noFrequency)
	g.Expect(err).To(MatchError(ContainSubstring("zone-dependent fields")))
}

func TestGovernancePerClusterFrequency(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	governor := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{}, Resources: vmopv1.VirtualMachineClassResources{Requests: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("2")), Max: ptr.To(resource.MustParse("4")), Default: ptr.To(resource.MustParse("2"))}}}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(governor).Build()
	vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}, Spec: vmopv1.VirtualMachineSpec{Resources: &vmopv1.VirtualMachineResourcesSpec{Requests: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse("4000"))}}}}
	allowed, err := filterGovernancePools(ctx, client, vm, map[string][]string{"a": {"slow", "fast"}}, func(pool string) (int64, error) {
		if pool == "fast" {
			return 3000, nil
		}
		return 1000, nil
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allowed).To(Equal(map[string][]string{"a": {"slow"}}))
}

func TestConcretePlacementAllocations(t *testing.T) {
	g := NewWithT(t)
	ctx := pkgcfg.NewContextWithDefaultConfig()
	scheme := runtime.NewScheme()
	g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	governor := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "g", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{}, Resources: vmopv1.VirtualMachineClassResources{
		Requests: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1"))}, Memory: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1Gi"))}},
		Limits:   vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Max: ptr.To(resource.MustParse("4"))}, Memory: &vmopv1.ResourceQuantityRange{Max: ptr.To(resource.MustParse("4Gi"))}},
	}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(governor).Build()
	for _, cpu := range []bool{false, true} {
		for _, limit := range []bool{false, true} {
			vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
			config := vimtypes.VirtualMachineConfigSpec{}
			allocation := &vimtypes.ResourceAllocationInfo{}
			if limit {
				allocation.Limit = ptr.To[int64](-1)
			} else {
				allocation.Reservation = ptr.To[int64](0)
			}
			if cpu {
				config.CpuAllocation = allocation
			} else {
				config.MemoryAllocation = allocation
			}
			placementResourceValues(vm, config)
			quantity := vm.Spec.Resources.Requests
			if limit {
				quantity = vm.Spec.Resources.Limits
			}
			value := quantity.Memory
			if cpu {
				value = quantity.CPU
			}
			g.Expect(value).NotTo(BeNil())
			expected := int64(0)
			if limit {
				expected = -1
			}
			g.Expect(value.Value()).To(Equal(expected))
			_, err := filterGovernancePools(ctx, client, vm, map[string][]string{"a": {"rp"}}, func(string) (int64, error) { return 1000, nil })
			g.Expect(err).To(MatchError(ContainSubstring("no zone satisfies")))
		}
	}
}
