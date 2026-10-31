// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"context"
	"encoding/json"
	"errors"
	"time"

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
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
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

	It("does not refresh boot defaults on an ordinary update after a class edit", func() {
		class.Spec.BootOptions = &vmopv1.VirtualMachineClassBootOptions{BootDelay: vmopv1.FixedClassLong(5000)}
		Expect(client.Update(ctx, class)).To(Succeed())
		vm.Spec.BootOptions = &vmopv1.VirtualMachineBootOptions{BootDelay: &metav1.Duration{Duration: time.Second}}
		oldVM := vm.DeepCopy()
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(vm.Spec.BootOptions.BootDelay.Duration).To(Equal(time.Second))
		Expect(calls).To(BeZero())
	})

	It("materializes native raw presets while typed defaults win without changing the class", func() {
		class.Spec.ConfigSpec = []byte(`{"_typeName":"VirtualMachineConfigSpec","cpuHotAddEnabled":true,"nestedHVEnabled":true}`)
		class.Spec.CPUAdvanced = &vmopv1.VirtualMachineClassCPUAdvancedSpec{HotAddEnabled: vmopv1.FixedClassBool(false)}
		Expect(client.Update(ctx, class)).To(Succeed())
		original := class.DeepCopy()
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.CPUAdvanced.HotAddEnabled).To(HaveValue(BeFalse()))
		Expect(vm.Spec.CPUAdvanced.NestedHardwareVirtualizationEnabled).To(HaveValue(BeTrue()))
		Expect(calls).To(Equal(1))
		Expect(client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
		Expect(class.Spec).To(Equal(original.Spec))

	})
	It("materializes a guaranteed legacy memory lock without an incompatible explicit request", func() {
		class.Spec.ConfigSpec = []byte(`{"memoryReservationLockedToMax":true}`)
		Expect(client.Update(ctx, class)).To(Succeed())
		defaults.Requests = &vmopv1.VirtualMachineResourceQuantity{Memory: resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)}
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.MemoryAdvanced.ReservationLockedToMax).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Resources.Requests.Memory).To(BeNil())
		Expect(defaults.Requests.Memory).NotTo(BeNil())
		class.Spec.ConfigSpec = nil
		class.Spec.MemoryAdvanced = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{ReservationLockedToMax: vmopv1.FixedClassBool(true)}
		Expect(client.Update(ctx, class)).To(Succeed())
		vm.Spec.MemoryAdvanced, vm.Spec.Resources = nil, nil
		_, err = vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.MemoryAdvanced.ReservationLockedToMax).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Resources.Requests.Memory).To(BeNil())

	})
	It("re-defaults a newly omitted hot-remove flag on a permitted class change", func() {
		oldVM := vm.DeepCopy()
		oldVM.Spec.ClassName = "previous"
		value := true
		vm.Spec.CPUAdvanced = &vmopv1.VirtualMachineCPUAdvancedSpec{HotRemoveEnabled: &value}
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.CPUAdvanced.HotRemoveEnabled).To(HaveValue(BeFalse()))
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
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		_, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
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
	It("normalizes first-class VMX bag defaults with explicit typed policy precedence", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		yes := true
		values := []common.KeyValuePair{{Key: "numa.vcpu.preferHT", Value: "FALSE"}, {Key: "sched.mem.lpage.enable1GPage", Value: "TRUE"}, {Key: "numa.nodeAffinity", Value: "0,2"}}
		class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "advanced", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{PreferHTEnabled: &vmopv1.BoolPolicy{Default: &yes}, ExtraConfig: &vmopv1.ExtraConfigListPolicy{Default: &values}}}}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()
		vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}}
		vm.Spec.ClassName = class.Name
		provider := resourceProvider(func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			return nil, nil
		})
		changed, err := vmclass.ApplyDefaults(ctx, client, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(vm.Spec.Advanced.PreferHTEnabled).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Advanced.HugePages1GEnabled).To(HaveValue(BeTrue()))
		Expect(vm.Spec.Advanced.PNUMANodeAffinity).To(Equal([]int32{0, 2}))
		Expect(vm.Spec.Advanced.ExtraConfig).To(BeEmpty())
		Expect(values[0].Value).To(Equal("FALSE"))
	})

	It("materializes defaults once and clears them when selecting an empty class", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true; c.Features.VMResizeCPUMemory = true })
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

var _ = Describe("Class boot defaults", Label(testlabels.Mutation, testlabels.API), func() {
	It("maps class booleans and milliseconds without changing VM-only boot choices", func() {
		vm := &vmopv1.VirtualMachine{}
		vm.Spec.BootOptions = &vmopv1.VirtualMachineBootOptions{
			Firmware:            vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI,
			NetworkBootProtocol: vmopv1.VirtualMachineBootOptionsNetworkBootProtocolIP6,
			BootOrder:           []vmopv1.VirtualMachineBootOptionsBootableDevice{{Type: vmopv1.VirtualMachineBootOptionsBootableCDRomDevice}},
		}
		defaults := vmopv1.VirtualMachineClassSpec{BootOptions: &vmopv1.VirtualMachineClassBootOptions{
			EFISecureBoot: vmopv1.FixedClassBool(false), BootRetry: vmopv1.FixedClassBool(true),
			BootDelay: vmopv1.FixedClassLong(0), BootRetryDelay: vmopv1.FixedClassLong(1500),
		}}
		Expect(applyTestBootDefaults(vm, defaults, false)).To(BeTrue())
		Expect(vm.Spec.BootOptions.Firmware).To(Equal(vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI))
		Expect(vm.Spec.BootOptions.NetworkBootProtocol).To(Equal(vmopv1.VirtualMachineBootOptionsNetworkBootProtocolIP6))
		Expect(vm.Spec.BootOptions.BootOrder).To(Equal([]vmopv1.VirtualMachineBootOptionsBootableDevice{{Type: vmopv1.VirtualMachineBootOptionsBootableCDRomDevice}}))
		Expect(vm.Spec.BootOptions.EFISecureBoot).To(Equal(vmopv1.VirtualMachineBootOptionsEFISecureBootDisabled))
		Expect(vm.Spec.BootOptions.BootRetry).To(Equal(vmopv1.VirtualMachineBootOptionsBootRetryEnabled))
		Expect(vm.Spec.BootOptions.BootDelay.Duration).To(BeZero())
		Expect(vm.Spec.BootOptions.BootRetryDelay.Duration).To(Equal(1500 * time.Millisecond))
	})
	DescribeTable("resolves class secure boot firmware without persisting an override",
		func(imageFirmware, rawFirmware string, explicit vmopv1.VirtualMachineBootOptionsFirmwareType, missing bool, expected vmopv1.VirtualMachineBootOptionsFirmwareType) {
			vm := &vmopv1.VirtualMachine{}
			vm.Spec.BootOptions = &vmopv1.VirtualMachineBootOptions{Firmware: explicit}
			var objects []ctrlclient.Object
			if imageFirmware != "" || missing {
				vm.Spec.Image = &vmopv1.VirtualMachineImageRef{Kind: "VirtualMachineImage", Name: "image"}
				if !missing {
					objects = append(objects, &vmopv1.VirtualMachineImage{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "image"}, Status: vmopv1.VirtualMachineImageStatus{Firmware: imageFirmware}})
				}
			}
			spec := vmopv1.VirtualMachineClassSpec{BootOptions: &vmopv1.VirtualMachineClassBootOptions{EFISecureBoot: vmopv1.FixedClassBool(true)}}
			if rawFirmware != "" {
				spec.ConfigSpec = []byte(`{"firmware":"` + rawFirmware + `"}`)
			}
			_, err := applyTestBootDefaultsWithObjects(vm, spec, false, objects...)
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Spec.BootOptions.Firmware).To(Equal(explicit))
			scheme := runtime.NewScheme()
			Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
			class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Namespace: vm.Namespace, Name: vm.Spec.ClassName}, Spec: spec}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objects, class)...).Build()
			firmware, err := vmclass.ResolveBootFirmware(pkgcfg.NewContextWithDefaultConfig(), c, vm, nil)
			if missing {
				Expect(err).To(MatchError(ContainSubstring("resolve image firmware")))
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(firmware).To(Equal(string(expected)))
			}
			Expect(vm.Spec.BootOptions.Firmware).To(Equal(explicit))
		},
		Entry("image EFI", "efi", "", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI),
		Entry("raw EFI fallback without image", "", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI),
		Entry("image BIOS overrides raw EFI", "bios", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareTypeBIOS),
		Entry("explicit BIOS is preserved", "efi", "efi", vmopv1.VirtualMachineBootOptionsFirmwareTypeBIOS, false, vmopv1.VirtualMachineBootOptionsFirmwareTypeBIOS),
		Entry("missing image does not fall back to raw EFI", "", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), true, vmopv1.VirtualMachineBootOptionsFirmwareType("")),
	)
	It("rechecks raw firmware on class changes and uses the selected immutable snapshot", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) {
			c.Features.VMServiceClasslessVM = true
			c.Features.ImmutableClasses = true
			c.Features.VMResizeCPUMemory = true
		})
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		oldSpec := vmopv1.VirtualMachineClassSpec{ConfigSpec: []byte(`{"firmware":"efi","bootOptions":{"efiSecureBootEnabled":true}}`)}
		newClass := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "bios", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{ConfigSpec: []byte(`{"firmware":"bios"}`)}}
		snapshot := &vmopv1.VirtualMachineClassInstance{ObjectMeta: metav1.ObjectMeta{Name: "snapshot", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassInstanceSpec{VirtualMachineClassSpec: oldSpec}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newClass, snapshot).Build()
		vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant"}, Spec: vmopv1.VirtualMachineSpec{ClassName: "old", Class: &common.LocalObjectRef{Name: "snapshot"}}}
		provider := resourceProvider(func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
			return nil, nil
		})
		_, err := vmclass.ApplyDefaults(ctx, c, provider, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.BootOptions.Firmware).To(BeEmpty())
		firmware, err := vmclass.ResolveBootFirmware(ctx, c, vm, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(firmware).To(Equal("efi"))
		oldVM := vm.DeepCopy()
		vm.Spec.ClassName = "bios"
		_, err = vmclass.ApplyDefaults(ctx, c, provider, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Spec.BootOptions.Firmware).To(BeEmpty())
		firmware, err = vmclass.ResolveBootFirmware(ctx, c, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(firmware).To(Equal("bios"))
		vm.Spec.Class = &common.LocalObjectRef{Name: "missing"}
		_, err = vmclass.ResolveBootFirmware(ctx, c, vm, nil)
		Expect(err).To(MatchError(ContainSubstring("resolve class instance")))
		vm.Spec.Class = nil
		vm.Spec.ClassName = "missing"
		_, err = vmclass.ResolveBootFirmware(ctx, c, vm, nil)
		Expect(err).To(MatchError(ContainSubstring("class not found")))
	})
	DescribeTable("materializes retry delays only for an enabled effective retry",
		func(raw, retrySet, enabled, tenantEnabled, reset bool) {
			vm := &vmopv1.VirtualMachine{}
			if tenantEnabled {
				vm.Spec.BootOptions = &vmopv1.VirtualMachineBootOptions{BootRetry: vmopv1.VirtualMachineBootOptionsBootRetryEnabled, BootRetryDelay: &metav1.Duration{Duration: time.Second}}
			}
			spec := vmopv1.VirtualMachineClassSpec{BootOptions: &vmopv1.VirtualMachineClassBootOptions{BootRetryDelay: vmopv1.FixedClassLong(1500)}}
			if retrySet {
				spec.BootOptions.BootRetry = vmopv1.FixedClassBool(enabled)
			}
			if raw {
				var err error
				spec.ConfigSpec, err = vmopv1.ProjectClassConfigSpec(&spec)
				Expect(err).NotTo(HaveOccurred())
				spec.BootOptions = nil
			}
			applyTestBootDefaults(vm, spec, reset)
			if !reset && tenantEnabled && !retrySet {
				Expect(vm.Spec.BootOptions.BootRetry).To(Equal(vmopv1.VirtualMachineBootOptionsBootRetryEnabled))
				Expect(vm.Spec.BootOptions.BootRetryDelay.Duration).To(Equal(1500 * time.Millisecond))
			} else if vm.Spec.BootOptions != nil {
				Expect(vm.Spec.BootOptions.BootRetryDelay).To(BeNil())
			}
			// Materialization must not discard an inactive class/provider preset.
			projected, err := vmopv1.ProjectClassConfigSpec(&spec)
			Expect(err).NotTo(HaveOccurred())
			doc, err := vmopv1.ParseClassConfigSpec(projected)
			Expect(err).NotTo(HaveOccurred())
			var delay int64
			present, err := doc.Get("bootOptions.bootRetryDelay", &delay)
			Expect(err).NotTo(HaveOccurred())
			Expect(present).To(BeTrue())
			Expect(delay).To(Equal(int64(1500)))
		},
		Entry("typed disabled", false, true, false, false, false),
		Entry("raw disabled", true, true, false, false, false),
		Entry("typed omitted", false, false, false, false, false),
		Entry("raw omitted", true, false, false, false, false),
		Entry("typed omitted preserves tenant enabled", false, false, false, true, false),
		Entry("raw omitted preserves tenant enabled", true, false, false, true, false),
		Entry("typed disabled clears previous enabled on class change", false, true, false, true, true),
		Entry("raw disabled clears previous enabled on class change", true, true, false, true, true),
		Entry("omitted clears previous enabled on class change", false, false, false, true, true),
	)
	It("keeps inline values when the create class has no default and clears them on a class change", func() {
		vm := &vmopv1.VirtualMachine{}
		vm.Spec.BootOptions = &vmopv1.VirtualMachineBootOptions{BootDelay: &metav1.Duration{Duration: time.Second}, Firmware: vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI}
		Expect(applyTestBootDefaults(vm, vmopv1.VirtualMachineClassSpec{}, false)).To(BeFalse())
		Expect(applyTestBootDefaults(vm, vmopv1.VirtualMachineClassSpec{}, true)).To(BeTrue())
		Expect(vm.Spec.BootOptions.BootDelay).To(BeNil())
		Expect(vm.Spec.BootOptions.Firmware).To(Equal(vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI))
	})
})

func applyTestBootDefaults(vm *vmopv1.VirtualMachine, spec vmopv1.VirtualMachineClassSpec, reset bool) bool {
	changed, err := applyTestBootDefaultsWithObjects(vm, spec, reset)
	Expect(err).NotTo(HaveOccurred())
	return changed
}

func applyTestBootDefaultsWithObjects(vm *vmopv1.VirtualMachine, spec vmopv1.VirtualMachineClassSpec, reset bool, objects ...ctrlclient.Object) (bool, error) {
	ctx := pkgcfg.NewContextWithDefaultConfig()
	pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) {
		c.Features.VMServiceClasslessVM = true
		c.Features.VMResizeCPUMemory = true
	})
	scheme := runtime.NewScheme()
	Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	vm.Namespace, vm.Spec.ClassName = "tenant", "boot-class"
	class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: vm.Spec.ClassName, Namespace: vm.Namespace}, Spec: spec}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(objects, class)...).Build()
	var oldVM *vmopv1.VirtualMachine
	if reset {
		oldVM = vm.DeepCopy()
		oldVM.Spec.ClassName = "old-class"
	}
	provider := resourceProvider(func(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error) {
		return nil, nil
	})
	return vmclass.ApplyDefaults(ctx, client, provider, vm, oldVM)
}

var _ = Describe("Device policy defaults", Label(testlabels.API), func() {
	It("preserves absent, empty and populated defaults for both device types", func() {
		Expect(vmclass.ListDefaults[vmopv1.VGPUDevice](nil)).To(BeNil())
		var absent *vmopv1.VGPUDeviceListPolicy
		Expect(vmclass.ListDefaults(absent)).To(BeNil())
		Expect(vmclass.ListDefaults(&vmopv1.VGPUDeviceListPolicy{})).To(BeNil())
		empty := []vmopv1.VGPUDevice{}
		Expect(vmclass.ListDefaults(&vmopv1.VGPUDeviceListPolicy{Default: &empty})).NotTo(BeNil())
		devices := []vmopv1.VGPUDevice{{ProfileName: "gpu-a"}}
		Expect(vmclass.ListDefaults(&vmopv1.VGPUDeviceListPolicy{Default: &devices})).To(Equal(devices))
		var direct *vmopv1.DynamicDirectPathIODeviceListPolicy
		Expect(vmclass.ListDefaults(direct)).To(BeNil())
		emptyDirect := []vmopv1.DynamicDirectPathIODevice{}
		Expect(vmclass.ListDefaults(&vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &emptyDirect})).NotTo(BeNil())
		directDevices := []vmopv1.DynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, Count: 2}}
		Expect(vmclass.ListDefaults(&vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &directDevices})).To(Equal(directDevices))
	})
})

var _ = Describe("Class configuration hashes", Label(testlabels.API), func() {
	It("ignores governance metadata but rotates for typed policy changes", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		spec := &vmopv1.VirtualMachineClassSpec{CPUAdvanced: &vmopv1.VirtualMachineClassCPUAdvancedSpec{HotAddEnabled: vmopv1.FixedClassBool(false)}}
		before, err := vmclass.ConfigurationHash(ctx, spec)
		Expect(err).NotTo(HaveOccurred())
		spec.Description = "updated"
		spec.Zones = &[]string{"a"}
		spec.Governs = &vmopv1.VirtualMachineClassGovernance{ExistingVMs: "DenyPowerOn"}
		spec.ReservedSlots = 10 //nolint:staticcheck // Legacy metadata must not rotate configuration snapshots.
		after, err := vmclass.ConfigurationHash(ctx, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
		*spec.CPUAdvanced.HotAddEnabled.AllowOverride = true
		after, err = vmclass.ConfigurationHash(ctx, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).NotTo(Equal(before))
	})
})

var _ = Describe("Class admission defaults", Label(testlabels.Mutation, testlabels.API), func() {
	DescribeTable("locks memory only for effective attaching device presets", func(raw string, locked bool) {
		var spec vmopv1.VirtualMachineClassSpec
		Expect(json.Unmarshal([]byte(raw), &spec)).To(Succeed())
		before := spec.DeepCopy()
		changed, err := vmclass.ApplyClassDefaults(&spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(Equal(locked))
		if locked {
			Expect(spec.MemoryAdvanced.ReservationLockedToMax).To(Equal(vmopv1.FixedClassBool(true)))
		} else {
			Expect(spec.MemoryAdvanced).To(Equal(before.MemoryAdvanced))
		}
		Expect(spec.Hardware).To(Equal(before.Hardware))
		Expect(spec.Resources).To(Equal(before.Resources))
		Expect(spec.ConfigSpec).To(Equal(before.ConfigSpec))
		changed, err = vmclass.ApplyClassDefaults(&spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse(), "defaulting must converge")
	},
		Entry("no device", `{}`, false),
		Entry("vgpu preset", `{"hardware":{"devices":{"vgpuDevices":{"default":[{"profileName":"gpu"}]}}}}`, true),
		Entry("DirectPath preset", `{"hardware":{"devices":{"dynamicDirectPathIODevices":{"default":[{"vendorID":1,"deviceID":2,"count":2}]}}}}`, true),
		Entry("empty device defaults", `{"hardware":{"devices":{"vgpuDevices":{"default":[]},"dynamicDirectPathIODevices":{"default":[]}}}}`, false),
		Entry("allowed-only policy", `{"hardware":{"devices":{"vgpuDevices":{"allowed":[{}]},"dynamicDirectPathIODevices":{"denied":[{}]}}}}`, false),
		Entry("raw config suppresses typed devices", `{"configSpec":{},"hardware":{"devices":{"vgpuDevices":{"default":[{"profileName":"gpu"}]}}}}`, false),
		Entry("raw added PCI device", `{"configSpec":{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"add","device":{"_typeName":"VirtualPCIPassthrough"}}]}}`, true),
		Entry("raw removed PCI device", `{"configSpec":{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"remove","device":{"_typeName":"VirtualPCIPassthrough"}}]}}`, false),
		Entry("raw edited SR-IOV device", `{"configSpec":{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"edit","device":{"_typeName":"VirtualSriovEthernetCard"}}]}}`, true),
		Entry("raw ordinary NIC", `{"configSpec":{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"add","device":{"_typeName":"VirtualVmxnet3"}}]}}`, false),
		Entry("replace explicit false without changing resource ranges", `{"hardware":{"devices":{"vgpuDevices":{"default":[{"profileName":"gpu"}]}}},"memoryAdvanced":{"reservationLockedToMax":{"default":false,"allowOverride":true}},"resources":{"requests":{"memory":{"default":"1Gi"}}}}`, true),
	)
	It("defaults only omitted governance enforcement", func() {
		spec := vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &[]string{}}}
		changed, err := vmclass.ApplyClassDefaults(&spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		Expect(spec.Governs.Enforcement).To(Equal(vmopv1.VirtualMachineClassEnforcementDeny))
		Expect(spec.Governs.Zones).To(HaveValue(BeEmpty()))
		Expect(spec.Governs.ExistingVMs).To(BeEmpty())
		spec.Governs.Enforcement = "invalid"
		changed, err = vmclass.ApplyClassDefaults(&spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse(), "invalid explicit policy belongs to validation")
		Expect(spec.Governs.Enforcement).To(Equal(vmopv1.VirtualMachineClassEnforcement("invalid")))
	})
	It("rejects null raw device changes without panicking", func() {
		spec := vmopv1.VirtualMachineClassSpec{ConfigSpec: []byte(`{"deviceChange":[null]}`)}
		changed, err := vmclass.ApplyClassDefaults(&spec)
		Expect(err).To(MatchError(ContainSubstring("read class device presets")))
		Expect(changed).To(BeFalse())
	})
	It("returns malformed raw device data without partially defaulting", func() {
		spec := vmopv1.VirtualMachineClassSpec{ConfigSpec: []byte(`{"deviceChange":"invalid"}`), Governs: &vmopv1.VirtualMachineClassGovernance{}}
		before := spec.DeepCopy()
		changed, err := vmclass.ApplyClassDefaults(&spec)
		Expect(err).To(MatchError(ContainSubstring("read class device presets")))
		Expect(changed).To(BeFalse())
		Expect(spec).To(Equal(*before))
	})
})
