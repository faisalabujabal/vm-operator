// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

type hardwareFixture struct {
	class   vmopv1.VirtualMachineClass
	ids     []string
	targets []vimv1.ConfigTarget
	options []vimv1.VirtualMachineConfigOptions
	guests  []vimv1.VirtualMachineGuestOptions
}

func newHardwareFixture() hardwareFixture {
	f := hardwareFixture{ids: []string{"cluster-a"}}
	f.class.Generation = 7
	f.class.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("2"))
	f.class.Spec.Resources.Size.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
	f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: "gpu-a"}}}
	f.targets = []vimv1.ConfigTarget{{ObjectMeta: metav1.ObjectMeta{Name: "a", UID: "a", Generation: 1}, Spec: vimv1.ConfigTargetSpec{ID: vimv1.ManagedObjectID{ID: "cluster-a"}}, Status: vimv1.ConfigTargetStatus{
		ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: vimv1.ReadyConditionType, Status: metav1.ConditionTrue}},
		MaxHardwareVersion: "vmx-21", MaxCPUsPerVM: 8, SupportedMaxMem: ptr.To(resource.MustParse("8Gi")),
		ConfigTargetDevices: vimv1.ConfigTargetDevices{VGPUProfile: []vimv1.VirtualMachineVgpuProfileInfo{{ProfileName: "gpu-a"}}},
	}}}
	f.options = []vimv1.VirtualMachineConfigOptions{{ObjectMeta: metav1.ObjectMeta{Name: "vmx-21", UID: "options", Generation: 1, Annotations: map[string]string{"vmoperator.vmware.com/environment-browser-cluster": "cluster-a"}, OwnerReferences: []metav1.OwnerReference{{UID: "a"}}}, Spec: vimv1.VirtualMachineConfigOptionsSpec{HardwareVersion: "vmx-21"}, Status: vimv1.VirtualMachineConfigOptionsStatus{
		ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: vimv1.ReadyConditionType, Status: metav1.ConditionTrue}},
		GuestOSIdentifiers: []vimv1.VirtualMachineGuestOSIdentifier{"otherLinux64Guest"},
		HardwareOptions:    &vimv1.VirtualHardwareOption{NumCPU: []int32{1, 2, 4, 8}, Memory: &vimv1.ResourceQuantityOption{ResourceQuantityRange: vimv1.ResourceQuantityRange{Min: resource.MustParse("128Mi"), Max: resource.MustParse("8Gi")}}},
		Capabilities:       &vimv1.VirtualMachineConfigCapability{},
	}}}
	return f
}

func TestHardwareCompatibility(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*hardwareFixture)
		status metav1.ConditionStatus
		field  string
	}{
		{"supported defaults", func(_ *hardwareFixture) {}, metav1.ConditionTrue, ""},
		{"absent profile", func(f *hardwareFixture) { f.targets[0].Status.VGPUProfile = nil }, metav1.ConditionFalse, "hardware.devices.vgpuDevices"},
		{"legacy profile inventory", func(f *hardwareFixture) {
			f.targets[0].Status.VGPUProfile = nil
			f.targets[0].Status.SharedGPUPassthroughTypes = []vimv1.VirtualMachinePciSharedGpuPassthroughInfo{{VGPU: "gpu-a"}}
		}, metav1.ConditionTrue, ""},
		{"missing zone", func(f *hardwareFixture) { f.ids = nil }, metav1.ConditionUnknown, ""},
		{"missing target", func(f *hardwareFixture) { f.targets = nil }, metav1.ConditionUnknown, ""},
		{"duplicate target identity", func(f *hardwareFixture) { f.targets = append(f.targets, *f.targets[0].DeepCopy()) }, metav1.ConditionUnknown, ""},
		{"stale target", func(f *hardwareFixture) { f.targets[0].Generation++ }, metav1.ConditionUnknown, ""},
		{"failed target", func(f *hardwareFixture) { f.targets[0].Status.Conditions[0].Status = metav1.ConditionFalse }, metav1.ConditionUnknown, ""},
		{"deleting target", func(f *hardwareFixture) { f.targets[0].DeletionTimestamp = ptr.To(metav1.Now()) }, metav1.ConditionUnknown, ""},
		{"missing options", func(f *hardwareFixture) { f.options = nil }, metav1.ConditionUnknown, ""},

		{"foreign options snapshot", func(f *hardwareFixture) {
			f.options[0].Annotations["vmoperator.vmware.com/environment-browser-cluster"] = "cluster-b"
		}, metav1.ConditionUnknown, ""},
		{"missing snapshot provenance", func(f *hardwareFixture) { f.options[0].Annotations = nil }, metav1.ConditionUnknown, ""},
		{"device-only class needs no options snapshot", func(f *hardwareFixture) {
			f.class.Spec.Resources.Size = vmopv1.VirtualMachineClassResourceSpec{}
			f.options = nil
		}, metav1.ConditionTrue, ""},
		{"source snapshot cannot reject another cluster", func(f *hardwareFixture) {
			f.options[0].Annotations["vmoperator.vmware.com/environment-browser-cluster"] = "cluster-b"
			f.options[0].Status.HardwareOptions.NumCPU = []int32{1}
		}, metav1.ConditionUnknown, ""},
		{"foreign options", func(f *hardwareFixture) { f.options[0].OwnerReferences[0].UID = "replaced" }, metav1.ConditionUnknown, ""},
		{"stale options", func(f *hardwareFixture) { f.options[0].Generation++ }, metav1.ConditionUnknown, ""},
		{"failed options", func(f *hardwareFixture) { f.options[0].Status.Conditions[0].Status = metav1.ConditionFalse }, metav1.ConditionUnknown, ""},
		{"missing hardware", func(f *hardwareFixture) { f.options[0].Status.HardwareOptions = nil }, metav1.ConditionUnknown, ""},
		{"target CPU ceiling", func(f *hardwareFixture) { f.targets[0].Status.MaxCPUsPerVM = 1 }, metav1.ConditionFalse, "resources.size.cpu"},
		{"CPU option", func(f *hardwareFixture) { f.options[0].Status.HardwareOptions.NumCPU = []int32{1} }, metav1.ConditionFalse, "resources.size.cpu"},
		{"missing CPU evidence", func(f *hardwareFixture) { f.targets[0].Status.MaxCPUsPerVM = 0 }, metav1.ConditionUnknown, ""},
		{"target memory ceiling", func(f *hardwareFixture) { f.targets[0].Status.SupportedMaxMem = ptr.To(resource.MustParse("1Gi")) }, metav1.ConditionFalse, "resources.size.memory"},
		{"memory option minimum", func(f *hardwareFixture) { f.options[0].Status.HardwareOptions.Memory.Min = resource.MustParse("4Gi") }, metav1.ConditionFalse, "resources.size.memory"},
		{"bounds resolved separately", func(f *hardwareFixture) { f.class.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("128")) }, metav1.ConditionTrue, ""},
		{"unknown raw configuration", func(f *hardwareFixture) { f.class.Spec.ConfigSpec = []byte(`{"numCPUs":"invalid"}`) }, metav1.ConditionUnknown, ""},
		{"raw CPU fallback", func(f *hardwareFixture) {
			f.class.Spec.Resources.Size.CPU = nil
			f.class.Spec.ConfigSpec = []byte(`{"numCPUs":64}`)
		}, metav1.ConditionFalse, "resources.size.cpu"},
		{"typed CPU precedence", func(f *hardwareFixture) { f.class.Spec.ConfigSpec = []byte(`{"numCPUs":64}`) }, metav1.ConditionTrue, ""},
		{"raw memory fallback", func(f *hardwareFixture) {
			f.class.Spec.Resources.Size.Memory = nil
			f.class.Spec.ConfigSpec = []byte(`{"memoryMB":16384}`)
		}, metav1.ConditionFalse, "resources.size.memory"},
		{"raw suppresses typed devices", func(f *hardwareFixture) {
			f.class.Spec.ConfigSpec = []byte(`{}`)
			f.targets[0].Status.VGPUProfile = nil
		}, metav1.ConditionTrue, ""},
		{"raw vGPU unsupported", func(f *hardwareFixture) {
			f.class.Spec.ConfigSpec = []byte(`{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"add","device":{"_typeName":"VirtualPCIPassthrough","key":-1,"backing":{"_typeName":"VirtualPCIPassthroughVmiopBackingInfo","vgpu":"missing"}}}]}`)
		}, metav1.ConditionFalse, "configSpec.deviceChange"},
		{"unsupported version", func(f *hardwareFixture) { f.class.Spec.ConfigSpec = []byte(`{"version":"vmx-99"}`) }, metav1.ConditionFalse, "configSpec.version"},
		{"unknown version", func(f *hardwareFixture) { f.targets[0].Status.MaxHardwareVersion = "" }, metav1.ConditionUnknown, ""},
		{"nested virtualization unsupported", func(f *hardwareFixture) {
			f.class.Spec.CPUAdvanced = &vmopv1.VirtualMachineClassCPUAdvancedSpec{NestedHardwareVirtualizationEnabled: &vmopv1.BoolPolicy{Default: ptr.To(true)}}
		}, metav1.ConditionFalse, "cpuAdvanced.nestedHardwareVirtualizationEnabled"},
		{"missing secure boot evidence", func(f *hardwareFixture) {
			f.class.Spec.BootOptions = &vmopv1.VirtualMachineClassBootOptions{EFISecureBoot: &vmopv1.BoolPolicy{Default: ptr.To(true)}}
		}, metav1.ConditionUnknown, ""},
		{"raw boot retry unsupported", func(f *hardwareFixture) {
			f.class.Spec.ConfigSpec = []byte(`{"bootOptions":{"bootRetryEnabled":true}}`)
		}, metav1.ConditionFalse, "bootOptions.bootRetry"},
		{"missing guest", func(f *hardwareFixture) { f.class.Spec.ConfigSpec = []byte(`{"guestId":"otherLinux64Guest"}`) }, metav1.ConditionUnknown, ""},
		{"one good cluster", func(f *hardwareFixture) { addHardwareCluster(f); f.targets[0].Status.VGPUProfile = nil }, metav1.ConditionTrue, ""},
		{"one unknown cluster", func(f *hardwareFixture) { f.ids = append(f.ids, "missing"); f.targets[0].Status.VGPUProfile = nil }, metav1.ConditionUnknown, ""},
		{"no cross cluster mixing", func(f *hardwareFixture) {
			addHardwareCluster(f)
			f.targets[0].Status.VGPUProfile = nil
			f.targets[1].Status.MaxCPUsPerVM = 1
		}, metav1.ConditionFalse, "resources.size.cpu"},
		{"directpath unsigned identity", func(f *hardwareFixture) {
			f.class.Spec.Hardware.Devices.DynamicDirectPathIODevices = &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &[]vmopv1.DynamicDirectPathIODevice{{VendorID: 65535, DeviceID: 8, CustomLabel: "requested"}}}
			f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{{VendorID: -1, DeviceID: 8, CustomLabel: "requested"}}
		}, metav1.ConditionTrue, ""},
		{"directpath label mismatch", func(f *hardwareFixture) {
			f.class.Spec.Hardware.Devices.DynamicDirectPathIODevices = &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &[]vmopv1.DynamicDirectPathIODevice{{VendorID: 1, DeviceID: 8, CustomLabel: "requested"}}}
			f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{{VendorID: 1, DeviceID: 8, CustomLabel: "secret-label"}}
		}, metav1.ConditionFalse, "hardware.devices.dynamicDirectPathIODevices"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			f := newHardwareFixture()
			tc.edit(&f)
			before := f.class.DeepCopy()
			got := vmclass.HardwareCompatibility(&f.class, f.ids, f.targets, f.options, f.guests)
			g.Expect(got.Status).To(Equal(tc.status), got.Message)
			g.Expect(got.ObservedGeneration).To(Equal(int64(7)))
			g.Expect(got.Type).To(Equal(vmopv1.VirtualMachineClassConditionHardwareCompatible))
			g.Expect(f.class).To(Equal(*before))
			g.Expect(got.Message).NotTo(ContainSubstring("secret-label"))
			if tc.field != "" {
				g.Expect(got.Message).To(ContainSubstring(tc.field))
			}
		})
	}
}

func addHardwareCluster(f *hardwareFixture) {
	second := f.targets[0].DeepCopy()
	second.Name = "b"
	second.UID = types.UID("b")
	second.Spec.ID.ID = "cluster-b"
	f.targets = append(f.targets, *second)
	f.ids = append(f.ids, "cluster-b")
	secondOptions := f.options[0].DeepCopy()
	secondOptions.Name = "vmx-22"
	secondOptions.UID = "options-b"
	secondOptions.Spec.HardwareVersion = "vmx-22"
	secondOptions.OwnerReferences = []metav1.OwnerReference{{UID: "b"}}
	secondOptions.Annotations["vmoperator.vmware.com/environment-browser-cluster"] = "cluster-b"
	f.targets[1].Status.MaxHardwareVersion = "vmx-22"
	f.options = append(f.options, *secondOptions)
}

func TestHardwareGuestOptions(t *testing.T) {
	g := NewWithT(t)
	f := newHardwareFixture()
	f.class.Spec.ConfigSpec = []byte(`{"guestId":"otherLinux64Guest","firmware":"efi","cpuHotAddEnabled":true}`)
	f.guests = []vimv1.VirtualMachineGuestOptions{{Spec: vimv1.VirtualMachineGuestOptionsSpec{ID: "otherLinux64Guest"}, Status: vimv1.VirtualMachineGuestOptionsStatus{HardwareVersions: []vimv1.VirtualMachineGuestOptionsHardwareVersionStatus{{HardwareVersion: "vmx-21", SupportedMaxCPUs: 8, SupportedMinMem: ptr.To(resource.MustParse("128Mi")), SupportedMaxMem: ptr.To(resource.MustParse("8Gi")), SupportedFirmware: []vimv1.Firmware{vimv1.FirmwareEFI}, SupportsCpuHotAdd: true}}}}}
	evaluate := func() metav1.Condition {
		return vmclass.HardwareCompatibility(&f.class, f.ids, f.targets, f.options, f.guests)
	}
	g.Expect(evaluate().Status).To(Equal(metav1.ConditionTrue))
	f.guests[0].Spec.ID = vimv1.VirtualMachineGuestOSIdentifierOtherLinux64
	g.Expect(evaluate().Status).To(Equal(metav1.ConditionTrue))
	f.guests[0].Status.HardwareVersions[0].SupportsCpuHotAdd = false
	g.Expect(evaluate().Status).To(Equal(metav1.ConditionFalse))
	f.guests[0].Status.HardwareVersions = nil
	g.Expect(evaluate().Status).To(Equal(metav1.ConditionUnknown))
}

func TestHardwareRawDynamicIdentity(t *testing.T) {
	for _, extra := range []string{`"subVendorId":1`, `"subDeviceId":2`, `"revisionId":3`} {
		t.Run(extra, func(t *testing.T) {
			g := NewWithT(t)
			f := newHardwareFixture()
			f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{{VendorID: 1, DeviceID: 2}}
			setDevices := func(alternatives string) {
				f.class.Spec.ConfigSpec = []byte(fmt.Sprintf(`{"deviceChange":[{"_typeName":"VirtualDeviceConfigSpec","operation":"add","device":{"_typeName":"VirtualPCIPassthrough","key":-1,"backing":{"_typeName":"VirtualPCIPassthroughDynamicBackingInfo","allowedDevice":[%s]}}}]}`, alternatives))
			}
			limited := fmt.Sprintf(`{"vendorId":1,"deviceId":2,%s}`, extra)
			evaluate := func() metav1.ConditionStatus {
				return vmclass.HardwareCompatibility(&f.class, f.ids, f.targets, f.options, f.guests).Status
			}
			setDevices(limited)
			g.Expect(evaluate()).To(Equal(metav1.ConditionUnknown))
			setDevices(limited + `,{"vendorId":1,"deviceId":2}`)
			g.Expect(evaluate()).To(Equal(metav1.ConditionTrue))
			f.targets[0].Status.DynamicPassthroughDevices[0].DeviceID = 3
			g.Expect(evaluate()).To(Equal(metav1.ConditionFalse))
		})
	}
}
