// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"context"

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
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

var _ = Describe("Device memory reservation", func() {
	var ctx context.Context
	var vm, oldVM *vmopv1.VirtualMachine
	var class *vmopv1.VirtualMachineClass
	var scheme *runtime.Scheme
	BeforeEach(func() {
		ctx = pkgcfg.NewContextWithDefaultConfig()
		scheme = runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		vm = &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "vm", Namespace: "tenant"}}
		vm.Spec.ClassName = "class"
		class = &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "class", Namespace: "tenant"}}
		devices := []vmopv1.VGPUDevice{{ProfileName: "gpu"}}
		class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Default: &devices}
		class.Spec.Resources.Size.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		oldVM = nil
	})
	check := func(want bool, objects ...ctrlclient.Object) {
		GinkgoHelper()
		objects = append(objects, class)
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		before, classBefore := vm.DeepCopy(), class.DeepCopy()
		got, err := vmclass.DeviceMemoryReservationSatisfied(ctx, c, vm, oldVM)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(want))
		Expect(vm).To(Equal(before))
		Expect(class).To(Equal(classBefore))
	}
	It("rejects a class preset with no reservation", func() { check(false) })
	It("accepts a legacy fixed class with full reservation", func() {
		class.Spec.Resources.Requests.Memory = vmopv1.FixedClassResource(resource.MustParse("2048Mi"))
		check(true)
	})
	It("accepts a class memory lock", func() {
		class.Spec.MemoryAdvanced = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{ReservationLockedToMax: vmopv1.FixedClassBool(true)}
		check(true)
	})
	It("honors explicit VM false over a class lock", func() {
		class.Spec.MemoryAdvanced = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{ReservationLockedToMax: vmopv1.FixedClassBool(true)}
		vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(false)}
		check(false)
	})
	It("honors an explicit zero request over full class reservation", func() {
		class.Spec.Resources.Requests.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Requests: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("0"))}}
		check(false)
	})
	It("rejects a resize beyond the class reservation", func() {
		class.Spec.Resources.Requests.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("4Gi"))}}
		check(false)
	})
	It("checks inline DirectPath devices without a class", func() {
		vm.Spec.ClassName = ""
		vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: &vmopv1.VirtualMachineDevicesSpec{DynamicDirectPathIODevices: []vmopv1.VirtualMachineDynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, Count: 1}}}}
		check(false)
	})
	It("checks devices that remain attached after removal from desired configuration", func() {
		vm.Spec.ClassName = ""
		oldVM = vm.DeepCopy()
		oldVM.Status.Hardware = &vmopv1.VirtualMachineHardwareStatus{VGPUs: []vmopv1.VirtualMachineHardwareVGPUStatus{{}}}
		check(false)
	})
	It("checks raw passthrough presets", func() {
		class.Spec.Hardware.Devices.VGPUDevices = nil
		class.Spec.ConfigSpec = []byte(`{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"add","device":{"_typeName":"VirtualPCIPassthrough","backing":{"_typeName":"VirtualPCIPassthroughDynamicBackingInfo"}}}]}`)
		check(false)
	})
	It("accepts a raw full reservation", func() { class.Spec.ConfigSpec = []byte(`{"memoryAllocation":{"reservation":2048}}`); check(true) })
	It("does not use raw reservation after a typed limit replaces the allocation", func() {
		class.Spec.ConfigSpec = []byte(`{"memoryAllocation":{"reservation":2048}}`)
		class.Spec.Resources.Limits.Memory = vmopv1.FixedClassResource(resource.MustParse("4Gi"))
		check(false)
	})
	It("uses the immutable snapshot instead of the changed class", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.ImmutableClasses = true })
		class.Spec.Resources.Requests.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		instance := &vmopv1.VirtualMachineClassInstance{ObjectMeta: metav1.ObjectMeta{Name: "snapshot", Namespace: "tenant"}}
		instance.Spec.VirtualMachineClassSpec = *class.Spec.DeepCopy()
		instance.Spec.Resources.Requests.Memory = nil
		vm.Spec.Class = &common.LocalObjectRef{Name: "snapshot"}
		check(false, instance)
	})
	It("does not use a previous class snapshot after a class-name change", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.ImmutableClasses = true })
		vm.Spec.Class = &common.LocalObjectRef{Name: "old-snapshot"}
		oldVM = vm.DeepCopy()
		oldVM.Spec.ClassName = "old-class"
		class.Spec.Resources.Requests.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		check(true)
	})
	It("preserves delayed class-copy admission for a class without observed or inline devices", func() { class.Namespace = "other"; check(true) })
	It("reports unresolved snapshot errors", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.ImmutableClasses = true })
		vm.Spec.Class = &common.LocalObjectRef{Name: "missing"}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := vmclass.DeviceMemoryReservationSatisfied(ctx, c, vm, nil)
		Expect(err).To(HaveOccurred())
	})
})
