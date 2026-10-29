// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"context"
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
	DescribeTable("derives only the existing EFI prerequisite for class secure boot",
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
			if missing {
				Expect(err).To(MatchError(ContainSubstring("resolve image firmware")))
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Spec.BootOptions.Firmware).To(Equal(expected))
			}
		},
		Entry("image EFI", "efi", "", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI),
		Entry("raw EFI fallback without image", "", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI),
		Entry("image BIOS overrides raw EFI", "bios", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), false, vmopv1.VirtualMachineBootOptionsFirmwareType("")),
		Entry("explicit BIOS is preserved", "efi", "efi", vmopv1.VirtualMachineBootOptionsFirmwareTypeBIOS, false, vmopv1.VirtualMachineBootOptionsFirmwareTypeBIOS),
		Entry("missing image does not fall back to raw EFI", "", "efi", vmopv1.VirtualMachineBootOptionsFirmwareType(""), true, vmopv1.VirtualMachineBootOptionsFirmwareType("")),
	)
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
		c.Features.TelcoVMServiceAPI = true
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
