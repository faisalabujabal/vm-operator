// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

func quantity(s string) *resource.Quantity { q := resource.MustParse(s); return &q }
func ceilingCPU(minimum, maximum string) vmopv1.VirtualMachineClassSpec {
	return vmopv1.VirtualMachineClassSpec{Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: quantity(minimum), Max: quantity(maximum), Default: quantity(minimum)}}}}
}

var _ = Describe("Governance", Label(testlabels.API), func() {
	It("resolves only live governors in the namespace and rejects ambiguity", func() {
		classes := []vmopv1.VirtualMachineClass{{ObjectMeta: metav1.ObjectMeta{Name: "ceiling", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{}}}}
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&classes[0]).Build()
		got, err := governance.Resolve(context.Background(), client, "tenant", "zone-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal("ceiling"))
		got.Spec.Governs.ExistingVMs = "DenyPowerOn"
		Expect(classes[0].Spec.Governs.ExistingVMs).To(BeEmpty())
		got, err = governance.ResolveClasses(classes, "other", "zone-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeNil())
		classes = append(classes, *classes[0].DeepCopy())
		classes[1].Name = "duplicate"
		_, err = governance.ResolveClasses(classes, "tenant", "zone-a")
		Expect(errors.Is(err, governance.ErrMultipleGoverningClasses)).To(BeTrue())
		Expect(governance.GoverningNames(classes, "tenant", "zone-a")).To(Equal([]string{"ceiling", "duplicate"}))
		now := metav1.Now()
		classes[1].DeletionTimestamp = &now
		got, err = governance.ResolveClasses(classes, "tenant", "zone-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal("ceiling"))
	})
	DescribeTable("honors independent availability and governance zone selection", func(available, governing *[]string, expected bool) {
		c := &vmopv1.VirtualMachineClass{Spec: vmopv1.VirtualMachineClassSpec{Zones: available, Governs: &vmopv1.VirtualMachineClassGovernance{Zones: governing}}}
		Expect(governance.Governs(c, "a")).To(Equal(expected))
	}, Entry("all inherited", nil, nil, true), Entry("no available zones", &[]string{}, nil, false), Entry("governs none", nil, &[]string{}, false), Entry("intersection", &[]string{"b"}, &[]string{"a"}, false), Entry("explicit match", &[]string{"a"}, &[]string{"a"}, true))
	It("checks entire ranges, open bounds and exact quantity units without mutation", func() {
		ceiling := ceilingCPU("1", "4")
		class := ceilingCPU("2", "3")
		before := class.DeepCopy()
		Expect(governance.CheckClass(&class, &ceiling)).To(BeEmpty())
		Expect(class).To(Equal(*before))
		class.Resources.Size.CPU.Max = quantity("5")
		Expect(governance.CheckClass(&class, &ceiling)).To(HaveLen(1))
		class.Resources.Size.CPU.Max = nil
		Expect(governance.CheckClass(&class, &ceiling)).To(HaveLen(1))
		Expect(governance.CheckClass(&vmopv1.VirtualMachineClassSpec{}, &ceiling)).To(BeEmpty())
		ceiling.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{Min: quantity("1Gi"), Max: quantity("2Gi"), Default: quantity("1Gi")}
		vm := &vmopv1.VirtualMachineSpec{Resources: &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: quantity("4"), Memory: quantity("2048Mi")}}}
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(BeEmpty())
		vm.Resources.Size.Memory = quantity("2049Mi")
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
	})
	It("compares vCPU allocations against MHz exactly using the target frequency", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{Resources: vmopv1.VirtualMachineClassResources{Requests: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: quantity("500m"), Max: quantity("1500m"), Default: quantity("1")}}}}
		vm := &vmopv1.VirtualMachineSpec{Resources: &vmopv1.VirtualMachineResourcesSpec{Requests: &vmopv1.VirtualMachineResourceQuantity{CPU: quantity("1500")}}}
		Expect(governance.CheckVM(vm, &ceiling, 1000)).To(BeEmpty())
		Expect(governance.CheckVM(vm, &ceiling, 999)).To(HaveLen(1))
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
		vm.Resources.Requests.CPU = quantity("500")
		Expect(governance.CheckVM(vm, &ceiling, 1000)).To(BeEmpty())
		vm.Resources.Requests.CPU = quantity("499")
		Expect(governance.CheckVM(vm, &ceiling, 1000)).To(HaveLen(1))
	})
	It("handles unlimited limits and derives locked memory reservation from size", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{Resources: vmopv1.VirtualMachineClassResources{Limits: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Max: quantity("2")}}, Requests: vmopv1.VirtualMachineClassResourceSpec{Memory: vmopv1.FixedClassResource(*quantity("2Gi"))}}}
		vm := &vmopv1.VirtualMachineSpec{Resources: &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{Memory: quantity("2Gi")}, Limits: &vmopv1.VirtualMachineResourceQuantity{CPU: quantity("-1")}}, MemoryAdvanced: &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(true)}}
		errs := governance.CheckVM(vm, &ceiling, 1000)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Field).To(Equal("spec.resources.limits.cpu"))
		ceiling.Resources.Limits.CPU.Max = nil
		Expect(governance.CheckVM(vm, &ceiling, 1000)).To(BeEmpty())
	})
	It("distinguishes false booleans and missing effective values and converts boot units", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{CPUAdvanced: &vmopv1.VirtualMachineClassCPUAdvancedSpec{HotRemoveEnabled: vmopv1.FixedClassBool(false), Topology: &vmopv1.VirtualMachineClassCPUTopologySpec{CoresPerSocket: vmopv1.FixedClassInt(2)}}, BootOptions: &vmopv1.VirtualMachineClassBootOptions{BootRetry: vmopv1.FixedClassBool(false), BootDelay: vmopv1.FixedClassLong(1000)}}
		vm := &vmopv1.VirtualMachineSpec{CPUAdvanced: &vmopv1.VirtualMachineCPUAdvancedSpec{HotRemoveEnabled: ptr.To(false), Topology: &vmopv1.VirtualMachineCPUTopologySpec{CoresPerSocket: ptr.To(int32(2))}}, BootOptions: &vmopv1.VirtualMachineBootOptions{BootRetry: vmopv1.VirtualMachineBootOptionsBootRetryDisabled, BootDelay: &metav1.Duration{Duration: time.Second}}}
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(BeEmpty())
		vm.CPUAdvanced.HotRemoveEnabled = nil
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
		vm.CPUAdvanced.HotRemoveEnabled = ptr.To(false)
		vm.BootOptions.BootDelay.Duration = time.Second + time.Nanosecond
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
		c := ceiling.DeepCopy()
		c.CPUAdvanced.HotRemoveEnabled.AllowOverride = ptr.To(true)
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(1))
	})
	It("enforces enum required/allowed and order-independent NUMA sets", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{ReservedProfileName: &vmopv1.EnumPolicy{Allowed: &[]string{"a"}, Required: ptr.To(true)}, Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{PNUMANodeAffinity: &vmopv1.Int32SetPolicy{Allowed: &[]vmopv1.Int32Set{{Members: []int32{0, 2}}}, Required: ptr.To(true)}}}
		vm := &vmopv1.VirtualMachineSpec{ReservedProfileName: ptr.To("a"), Advanced: &vmopv1.VirtualMachineAdvancedSpec{PNUMANodeAffinity: []int32{2, 0}}}
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(BeEmpty())
		vm.ReservedProfileName = ptr.To("")
		vm.Advanced.PNUMANodeAffinity = []int32{1}
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(2))
		c := ceiling.DeepCopy()
		c.ReservedProfileName.Allowed = &[]string{"b"}
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(1))
		c = ceiling.DeepCopy()
		c.Advanced.PNUMANodeAffinity.Required = ptr.To(false)
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(1))
	})
	It("enforces list allow/deny matching for defaults and VM keys", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{ExtraConfig: &vmopv1.ExtraConfigListPolicy{Allowed: &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Glob: ptr.To("guestinfo.*")}}}, Denied: &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Regex: ptr.To("^guestinfo.secret")}}}}}}
		vm := &vmopv1.VirtualMachineSpec{Advanced: &vmopv1.VirtualMachineAdvancedSpec{ExtraConfig: []vmopv1common.KeyValuePair{{Key: "guestinfo.ok", Value: "value"}}}}
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(BeEmpty())
		vm.Advanced.ExtraConfig[0].Key = "guestinfo.secret"
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
		c := ceiling.DeepCopy()
		c.Advanced.ExtraConfig.Default = &vm.Advanced.ExtraConfig
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(1))
		ceiling.Advanced.ExtraConfig.Allowed = &[]vmopv1.ExtraConfigMatcher{}
		vm.Advanced.ExtraConfig[0].Key = "anything"
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
		ceiling.Advanced.ExtraConfig.Allowed = nil
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(BeEmpty())
		ceiling.Advanced.ExtraConfig.Denied = &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Regex: ptr.To("[")}}}
		vm.Advanced.ExtraConfig = nil
		Expect(governance.CheckVM(vm, &ceiling, 0)).To(HaveLen(1))
	})
	It("matches both device identities and rejects invalid matchers", func() {
		ceiling := vmopv1.VirtualMachineClassSpec{Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{VGPUDevices: &vmopv1.VGPUDeviceListPolicy{Allowed: &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Fixed: ptr.To("gpu")}}}}, DynamicDirectPathIODevices: &vmopv1.DynamicDirectPathIODeviceListPolicy{Allowed: &[]vmopv1.DynamicDirectPathIODeviceMatcher{{VendorID: ptr.To(int64(1)), DeviceID: ptr.To(int64(2)), CustomLabel: &vmopv1.StringMatcher{Glob: ptr.To("device-*")}}}}}}}
		c := ceiling.DeepCopy()
		c.Hardware.Devices.VGPUDevices.Default = &[]vmopv1.VGPUDevice{{ProfileName: "gpu"}}
		c.Hardware.Devices.DynamicDirectPathIODevices.Default = &[]vmopv1.DynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, CustomLabel: "device-a", Count: 2}}
		Expect(governance.CheckClass(c, &ceiling)).To(BeEmpty())
		(*c.Hardware.Devices.DynamicDirectPathIODevices.Default)[0].DeviceID = 3
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(1))
		(*ceiling.Hardware.Devices.VGPUDevices.Allowed)[0].ProfileName.Glob = ptr.To("*")
		Expect(governance.CheckClass(c, &ceiling)).To(HaveLen(2))
	})
})
