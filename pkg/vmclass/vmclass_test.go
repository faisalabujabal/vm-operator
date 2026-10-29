// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

type resourceProvider func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error)

func (f resourceProvider) GetVirtualMachineClassResources(ctx context.Context, spec vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
	return f(ctx, spec)
}

var _ = Describe("Class resource admission", Label(testlabels.Mutation, testlabels.API), func() {
	var (
		ctx      context.Context
		client   ctrlclient.Client
		vm       *vmopv1.VirtualMachine
		class    *vmopv1.VirtualMachineClass
		defaults *vmopv1.VirtualMachineResourcesSpec
		provider resourceProvider
		calls    int
	)
	BeforeEach(func() {
		ctx = pkgcfg.NewContextWithDefaultConfig()
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.TelcoVMServiceAPI = true })
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMResizeCPUMemory = true })
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		class = &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "small", Namespace: "tenant"}}
		class.Spec.Description = "live class"
		client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()
		vm = &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "tenant"}}
		vm.Spec.ClassName = class.Name
		defaults = &vmopv1.VirtualMachineResourcesSpec{
			Size: &vmopv1.VirtualMachineResourceQuantity{
				CPU:    resource.NewQuantity(2, resource.DecimalSI),
				Memory: resource.NewQuantity(2*1024*1024*1024, resource.BinarySI),
			},
		}
		calls = 0
		provider = func(_ context.Context, spec vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			calls++
			Expect(spec.Description).To(Equal("live class"))
			return defaults, nil
		}
	})
	It("materializes independent class-owned values and replaces conflicting create values", func() {
		vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: resource.NewQuantity(8, resource.DecimalSI)}}
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.Resources).To(Equal(defaults))
		vm.Spec.Resources.Size.CPU.Set(4)
		Expect(defaults.Size.CPU.Value()).To(Equal(int64(2)))
	})
	It("does not consult classes on ordinary updates or class removal", func() {
		oldVM := vm.DeepCopy()
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		vm.Spec.ClassName = ""
		changed, err = vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(calls).To(BeZero())
	})
	It("clears allocations from the previous class on a class change", func() {
		oldVM := vm.DeepCopy()
		oldVM.Spec.ClassName = "guaranteed"
		vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Requests: &vmopv1.VirtualMachineResourceQuantity{CPU: resource.NewQuantity(3000, resource.DecimalSI)}}
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.Resources.Requests).To(BeNil())
	})
	It("uses the selected immutable snapshot instead of the latest class", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.ImmutableClasses = true })
		instance := &vmopv1.VirtualMachineClassInstance{ObjectMeta: metav1.ObjectMeta{Name: "small-snapshot", Namespace: vm.Namespace}}
		instance.Spec.VirtualMachineClassSpec.Description = "snapshot"
		Expect(client.Create(ctx, instance)).To(Succeed())
		vm.Spec.Class = &common.LocalObjectRef{Name: instance.Name}
		provider = func(_ context.Context, spec vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			Expect(spec.Description).To(Equal("snapshot"))
			return defaults, nil
		}
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
	})
	It("does not use the previous class snapshot after className changes", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.ImmutableClasses = true })
		vm.Spec.Class = &common.LocalObjectRef{Name: "previous-snapshot"}
		oldVM := vm.DeepCopy()
		oldVM.Spec.ClassName = "previous"
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(1))
	})
	It("leaves existing objects untouched with capability or resize disabled", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.TelcoVMServiceAPI = false })
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.TelcoVMServiceAPI = true })
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMResizeCPUMemory = false })
		oldVM := vm.DeepCopy()
		oldVM.Spec.ClassName = "other"
		_, err = vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(BeZero())
	})
	It("reports provider failures but preserves admission before class arrival", func() {
		provider = func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			return nil, errors.New("frequency unavailable")
		}
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).To(MatchError(ContainSubstring("frequency unavailable")))
		Expect(vm.Spec.Resources).To(BeNil())
		vm.Spec.ClassName = "missing"
		_, err = vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.Resources).To(BeNil())
	})
})

var _ = Describe("Class advanced admission", Label(testlabels.Mutation, testlabels.API), func() {
	It("materializes defaults once and clears them when selecting an empty class", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.TelcoVMServiceAPI = true; c.Features.VMResizeCPUMemory = true })
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		yes, no := true, false
		members := []int32{0, 2}
		extra := []common.KeyValuePair{{Key: "example.class", Value: "default"}, {Key: "guestinfo.bootstrap", Value: "kept-in-class"}, {Key: "vmservice.test", Value: "kept-in-class"}, {Key: "ethernet0.ctxPerDev", Value: "kept-in-class"}}
		class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "advanced", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{
			ChangeBlockTracking: &vmopv1.BoolPolicy{Default: &no}, PreferHTEnabled: &vmopv1.BoolPolicy{Default: &yes}, HugePages1GEnabled: &vmopv1.BoolPolicy{Default: &no}, TimeTrackerLowLatencyEnabled: &vmopv1.BoolPolicy{Default: &yes}, CPUAffinityExclusiveNoStatsEnabled: &vmopv1.BoolPolicy{Default: &no}, VMXSwapEnabled: &vmopv1.BoolPolicy{Default: &yes}, PNUMANodeAffinity: &vmopv1.Int32SetPolicy{Default: &members}, ExtraConfig: &vmopv1.ExtraConfigListPolicy{Default: &extra},
		}}}
		empty := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "tenant"}}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class, empty).Build()
		vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "tenant"}}
		vm.Spec.ClassName = class.Name
		vm.Spec.Advanced = &vmopv1.VirtualMachineAdvancedSpec{ChangeBlockTracking: &yes, BootDiskCapacity: resource.NewQuantity(42, resource.BinarySI)}
		provider := resourceProvider(func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			return nil, nil
		})
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.Advanced.ChangeBlockTracking).To(HaveValue(BeFalse()))
		Expect(vm.Spec.Advanced.PreferHTEnabled).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Advanced.HugePages1GEnabled).To(HaveValue(BeFalse()))
		Expect(vm.Spec.Advanced.TimeTrackerLowLatencyEnabled).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Advanced.CPUAffinityExclusiveNoStatsEnabled).To(HaveValue(BeFalse()))
		Expect(vm.Spec.Advanced.VMXSwapEnabled).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Advanced.PNUMANodeAffinity).To(Equal(members))
		Expect(vm.Spec.Advanced.ExtraConfig).To(Equal(extra[:1]))
		vm.Spec.Advanced.PNUMANodeAffinity[0] = 3
		vm.Spec.Advanced.ExtraConfig[0].Value = "vm-edit"
		Expect(members).To(Equal([]int32{0, 2}))
		Expect(extra[0].Value).To(Equal("default"))
		old := vm.DeepCopy()
		changed, err = vmclass.ApplyDefaults(ctx, client, provider, vm, old)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(vm.Spec.Advanced.ExtraConfig[0].Value).To(Equal("vm-edit"))
		vm.Spec.ClassName = empty.Name
		changed, err = vmclass.ApplyDefaults(ctx, client, provider, vm, old)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.Advanced).To(Equal(&vmopv1.VirtualMachineAdvancedSpec{BootDiskCapacity: resource.NewQuantity(42, resource.BinarySI)}))
	})
})
