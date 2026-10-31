// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass_test

import (
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
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

var _ = Describe("Resolved class resource limits", Label(testlabels.API), func() {
	var f hardwareFixture
	var classes []vmopv1.VirtualMachineClass
	var resolve func(string) vmopv1.VirtualMachineClassResolvedField
	BeforeEach(func() {
		f = newHardwareFixture()
		f.class.Namespace = "tenant"
		f.class.Name = "selectable"
		f.class.Spec.Resources = vmopv1.VirtualMachineClassResources{}
		classes = nil
		resolve = func(path string) vmopv1.VirtualMachineClassResolvedField {
			for _, field := range vmclass.ResolveClassLimits(&f.class, classes, "zone-a", f.ids, f.targets, f.options, f.guests) {
				if field.Path == path {
					return field
				}
			}
			Fail("missing field " + path)
			return vmopv1.VirtualMachineClassResolvedField{}
		}
	})

	It("falls back to exact hardware CPU choices without inventing values between them", func() {
		field := resolve("resources.size.cpu")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.Quantity.Values).To(WithTransform(quantityCounts, Equal([]int64{1, 2, 4, 8})))
		Expect(field.Available.Quantity.Ranges).To(BeEmpty())
		Expect(field.Policy.Quantity.Ranges).To(HaveLen(1))
		Expect(field.Policy.Quantity.Ranges[0].Min).To(BeNil())
		Expect(field.Policy.Quantity.Ranges[0].Max).To(BeNil())
	})

	It("intersects selectable, governing and hardware bounds without changing inputs", func() {
		f.class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("2")), Max: ptr.To(resource.MustParse("16")), Default: ptr.To(resource.MustParse("4"))}
		governor := f.class.DeepCopy()
		governor.Name = "governor"
		governor.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		governor.Spec.Resources.Size.CPU.Min = ptr.To(resource.MustParse("3"))
		governor.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("6"))
		classes = []vmopv1.VirtualMachineClass{*governor}
		beforeClass, beforeGovernor := f.class.DeepCopy(), governor.DeepCopy()
		field := resolve("resources.size.cpu")
		Expect(field.Available.Quantity.Values).To(WithTransform(quantityCounts, Equal([]int64{4})))
		Expect(field.Policy.Quantity.Ranges[0].Min.Cmp(resource.MustParse("3"))).To(BeZero())
		Expect(field.Policy.Quantity.Ranges[0].Max.Cmp(resource.MustParse("6"))).To(BeZero())
		Expect(&f.class).To(Equal(beforeClass))
		Expect(&classes[0]).To(Equal(beforeGovernor))
	})

	It("does not turn a default-only policy into a fixed limit", func() {
		f.class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("4"))}
		Expect(resolve("resources.size.cpu").Available.Quantity.Values).To(HaveLen(4))
	})

	It("retains policy-only allocation constraints when no browser allocation domain exists", func() {
		f.class.Spec.Resources.Requests.CPU = vmopv1.FixedClassResource(resource.MustParse("2"))
		field := resolve("resources.requests.cpu")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Available).To(BeNil())
		Expect(field.Policy.Quantity.Ranges[0].Max.Cmp(resource.MustParse("2"))).To(BeZero())
		Expect(resolve("resources.limits.memory").State).To(Equal("Unknown"))
	})

	It("distinguishes a known empty CPU intersection from missing discovery", func() {
		f.class.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("3"))
		field := resolve("resources.size.cpu")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Reason).To(Equal("NoPermittedValues"))
		Expect(field.Available.Quantity.Values).To(BeEmpty())
		f.options = nil
		field = resolve("resources.size.cpu")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Available).To(BeNil())
		Expect(field.Policy.Quantity.Ranges).To(HaveLen(1))
	})

	It("suppresses partial choices if another cluster lacks authoritative options", func() {
		f.ids = append(f.ids, "cluster-b")
		field := resolve("resources.size.cpu")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Available).To(BeNil())
	})

	It("rejects ambiguous or stale options snapshots", func() {
		f.options = append(f.options, *f.options[0].DeepCopy())
		Expect(resolve("resources.size.cpu").State).To(Equal("Unknown"))
		f.options = f.options[:1]
		f.options[0].Generation++
		Expect(resolve("resources.size.cpu").State).To(Equal("Unknown"))
	})

	It("preserves gaps between memory intervals from different clusters", func() {
		f.options[0].Status.HardwareOptions.Memory.Min = resource.MustParse("1Gi")
		f.options[0].Status.HardwareOptions.Memory.Max = resource.MustParse("2Gi")
		target := f.targets[0].DeepCopy()
		target.UID, target.Spec.ID.ID = "b", "cluster-b"
		option := f.options[0].DeepCopy()
		option.OwnerReferences[0].UID = "b"
		option.Annotations["vmoperator.vmware.com/environment-browser-cluster"] = "cluster-b"
		option.Status.HardwareOptions.Memory.Min = resource.MustParse("4Gi")
		option.Status.HardwareOptions.Memory.Max = resource.MustParse("6Gi")
		f.ids = append(f.ids, "cluster-b")
		f.targets = append(f.targets, *target)
		f.options = append(f.options, *option)
		field := resolve("resources.size.memory")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.Quantity.Ranges).To(HaveLen(2))
		Expect(field.Available.Quantity.Ranges[0].Max.Cmp(resource.MustParse("2Gi"))).To(BeZero())
		Expect(field.Available.Quantity.Ranges[1].Min.Cmp(resource.MustParse("4Gi"))).To(BeZero())
	})

	It("never merges conflicting governing classes into a permissive policy", func() {
		governor := f.class.DeepCopy()
		governor.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		governor.Name = "one"
		classes = append(classes, *governor)
		governor.Name = "two"
		classes = append(classes, *governor)
		field := resolve("resources.size.cpu")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Reason).To(Equal("GovernanceUnresolved"))
		Expect(field.Policy).To(BeNil())
		Expect(field.Available).To(BeNil())
	})

	It("does not present a guest-specific class as resolved without guest evidence", func() {
		f.class.Spec.ConfigSpec = []byte(`{"_typeName":"VirtualMachineConfigSpec","guestId":"otherLinux64Guest"}`)
		Expect(resolve("resources.size.cpu").State).To(Equal("Unknown"))
		Expect(resolve("resources.size.memory").State).To(Equal("Unknown"))
	})

	It("does not accept a requested hardware version above the target maximum", func() {
		f.class.Spec.ConfigSpec = []byte(`{"_typeName":"VirtualMachineConfigSpec","version":"vmx-22"}`)
		f.options[0].Spec.HardwareVersion = "vmx-22"
		Expect(resolve("resources.size.cpu").State).To(Equal("Unknown"))
		Expect(resolve("resources.size.memory").Available).To(BeNil())
	})

	It("narrows memory with semantically equivalent quantity bounds", func() {
		f.class.Spec.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{
			Min: ptr.To(resource.MustParse("1024Mi")), Max: ptr.To(resource.MustParse("4096Mi")), Default: ptr.To(resource.MustParse("2Gi")),
		}
		field := resolve("resources.size.memory")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.Quantity.Ranges).To(HaveLen(1))
		Expect(field.Available.Quantity.Ranges[0].Min.Cmp(resource.MustParse("1Gi"))).To(BeZero())
		Expect(field.Available.Quantity.Ranges[0].Max.Cmp(resource.MustParse("4Gi"))).To(BeZero())
	})

	It("applies guest-specific CPU and memory ceilings", func() {
		f.class.Spec.ConfigSpec = []byte(`{"_typeName":"VirtualMachineConfigSpec","guestId":"otherLinux64Guest"}`)
		f.guests = []vimv1.VirtualMachineGuestOptions{{Spec: vimv1.VirtualMachineGuestOptionsSpec{ID: "otherLinux64Guest"},
			Status: vimv1.VirtualMachineGuestOptionsStatus{HardwareVersions: []vimv1.VirtualMachineGuestOptionsHardwareVersionStatus{{
				HardwareVersion: "vmx-21", SupportedMaxCPUs: 2, SupportedMinMem: ptr.To(resource.MustParse("1Gi")), SupportedMaxMem: ptr.To(resource.MustParse("4Gi")),
			}}},
		}}
		Expect(resolve("resources.size.cpu").Available.Quantity.Values).To(WithTransform(quantityCounts, Equal([]int64{1, 2})))
		Expect(resolve("resources.size.memory").Available.Quantity.Ranges[0].Max.Cmp(resource.MustParse("4Gi"))).To(BeZero())
	})

	It("does not expose discovery identifiers or pool totals", func() {
		fields := vmclass.ResolveClassLimits(&f.class, classes, "zone-a", f.ids, f.targets, f.options, f.guests)
		encoded, err := json.Marshal(fields)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).NotTo(ContainSubstring("cluster-a"))
		Expect(string(encoded)).NotTo(ContainSubstring("environment-browser-cluster"))
	})
})

func quantityCounts(values []resource.Quantity) []int64 {
	result := make([]int64, len(values))
	for i := range values {
		result[i] = values[i].Value()
	}
	return result
}

var _ = Describe("Resolved class device choices", Label(testlabels.API), func() {
	var f hardwareFixture
	var classes []vmopv1.VirtualMachineClass
	var resolve func(string) vmopv1.VirtualMachineClassResolvedField
	BeforeEach(func() {
		f = newHardwareFixture()
		f.class.Namespace, f.class.Name = "tenant", "selectable"
		f.class.Spec.Hardware.Devices = vmopv1.VirtualDevices{}
		f.targets[0].Status.VGPUProfile = []vimv1.VirtualMachineVgpuProfileInfo{
			{ProfileName: "gpu-a", DeviceVendorID: 123, FbSizeInGib: 4, ProfileSharing: "timeSliced", ProfileClass: "compute"},
			{ProfileName: "gpu-b"}, {ProfileName: "other"},
		}
		f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{
			{VendorID: 1, DeviceID: 2, CustomLabel: "fast", VendorName: "Vendor", DeviceName: "Model"},
			{VendorID: 1, DeviceID: 2, CustomLabel: "slow"},
			{VendorID: 3, DeviceID: 4, CustomLabel: "fast"},
		}
		classes = nil
		resolve = func(path string) vmopv1.VirtualMachineClassResolvedField {
			for _, field := range vmclass.ResolveClassLimits(&f.class, classes, "zone-a", f.ids, f.targets, f.options, f.guests) {
				if field.Path == path {
					return field
				}
			}
			Fail("missing device field " + path)
			return vmopv1.VirtualMachineClassResolvedField{}
		}
	})

	It("uses target identities without requiring configuration options", func() {
		f.options = nil
		vgpu := resolve("hardware.devices.vgpuDevices")
		Expect(vgpu.State).To(Equal("Complete"))
		Expect(vgpu.Available.VGPU.Values).To(HaveLen(3))
		direct := resolve("hardware.devices.dynamicDirectPathIODevices")
		Expect(direct.State).To(Equal("Complete"))
		Expect(direct.Available.DirectPath.Values).To(HaveLen(3))
	})

	It("intersects glob and regex allow policies and applies deny precedence", func() {
		f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{
			Allowed: &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Glob: ptr.To("gpu-*")}}},
		}
		governor := f.class.DeepCopy()
		governor.Name = "governor"
		governor.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		governor.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{
			Allowed: &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Regex: ptr.To("^(gpu-[ab]|other)$")}}},
			Denied:  &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Fixed: ptr.To("gpu-b")}}},
		}
		classes = []vmopv1.VirtualMachineClass{*governor}
		before := f.class.DeepCopy()
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.VGPU.Values).To(HaveLen(1))
		Expect(field.Available.VGPU.Values[0].ProfileName).To(Equal("gpu-a"))
		Expect(field.Policy.VGPU.Allowed).To(HaveLen(2))
		Expect(field.Policy.VGPU.Denied).To(HaveLen(1))
		Expect(&f.class).To(Equal(before))
	})

	It("keeps empty allowed distinct from absent allowed", func() {
		f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Allowed: &[]vmopv1.VGPUDeviceMatcher{}}
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.VGPU.Values).To(BeEmpty())
		Expect(field.Policy.VGPU.Allowed).To(HaveLen(1))
		f.class.Spec.Hardware.Devices.VGPUDevices.Allowed = nil
		Expect(resolve("hardware.devices.vgpuDevices").Available.VGPU.Values).To(HaveLen(3))
	})

	It("keeps a deny-only policy open for unmatched identities", func() {
		f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{
			Denied: &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Fixed: ptr.To("gpu-b")}}},
		}
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.Available.VGPU.Values).To(HaveLen(2))
		Expect(field.Available.VGPU.Values[0].ProfileName).To(Equal("gpu-a"))
		Expect(field.Available.VGPU.Values[1].ProfileName).To(Equal("other"))
	})

	It("distinguishes empty discovery from incomplete discovery", func() {
		f.targets[0].Status.VGPUProfile = nil
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Complete"))
		Expect(field.Available.VGPU.Values).To(BeEmpty())
		f.ids = append(f.ids, "missing-cluster")
		field = resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Available).To(BeNil())
	})

	It("does not mistake default devices for an identity restriction", func() {
		f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: "gpu-a"}}}
		f.class.Spec.Hardware.Devices.DynamicDirectPathIODevices = &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &[]vmopv1.DynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, CustomLabel: "fast", Count: 1}}}
		Expect(resolve("hardware.devices.vgpuDevices").Available.VGPU.Values).To(HaveLen(3))
		Expect(resolve("hardware.devices.dynamicDirectPathIODevices").Available.DirectPath.Values).To(HaveLen(3))
	})

	It("filters numeric device identity and backing labels independently", func() {
		f.class.Spec.Hardware.Devices.DynamicDirectPathIODevices = &vmopv1.DynamicDirectPathIODeviceListPolicy{
			Allowed: &[]vmopv1.DynamicDirectPathIODeviceMatcher{{VendorID: ptr.To(int64(1))}},
			Denied:  &[]vmopv1.DynamicDirectPathIODeviceMatcher{{CustomLabel: &vmopv1.StringMatcher{Fixed: ptr.To("slow")}}},
		}
		field := resolve("hardware.devices.dynamicDirectPathIODevices")
		Expect(field.Available.DirectPath.Values).To(Equal([]vmopv1.VirtualMachineClassResolvedDirectPath{
			{VendorID: 1, DeviceID: 2, CustomLabel: "fast", VendorName: "Vendor", DeviceName: "Model"},
		}))
	})

	It("reports unknown for missing, duplicated or stale target snapshots", func() {
		for _, edit := range []func(){
			func() { f.targets = nil },
			func() { f.targets = append(f.targets, *f.targets[0].DeepCopy()) },
			func() { f.targets[0].Generation++ },
		} {
			original := f.targets[0].DeepCopy()
			edit()
			for _, path := range []string{"hardware.devices.vgpuDevices", "hardware.devices.dynamicDirectPathIODevices"} {
				field := resolve(path)
				Expect(field.State).To(Equal("Unknown"))
				Expect(field.Available).To(BeNil())
			}
			f.targets = []vimv1.ConfigTarget{*original}
		}
	})

	It("normalizes signed PCI IDs before matching policies", func() {
		f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{{VendorID: -1, DeviceID: -32768}}
		f.class.Spec.Hardware.Devices.DynamicDirectPathIODevices = &vmopv1.DynamicDirectPathIODeviceListPolicy{
			Allowed: &[]vmopv1.DynamicDirectPathIODeviceMatcher{{VendorID: ptr.To(int64(65535)), DeviceID: ptr.To(int64(32768))}},
		}
		values := resolve("hardware.devices.dynamicDirectPathIODevices").Available.DirectPath.Values
		Expect(values).To(HaveLen(1))
		Expect(values[0].VendorID).To(Equal(int64(65535)))
		Expect(values[0].DeviceID).To(Equal(int64(32768)))
	})

	It("rejects malformed identity evidence instead of wrapping it to another device", func() {
		f.targets[0].Status.DynamicPassthroughDevices = []vimv1.VirtualMachineDynamicPassthroughInfo{{VendorID: 65537, DeviceID: 2}}
		field := resolve("hardware.devices.dynamicDirectPathIODevices")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Available).To(BeNil())
	})

	It("retains display fields but never embeds target inventory metadata", func() {
		f.targets[0].Status.VGPUProfile[0].Name = "secret-host-inventory"
		f.targets[0].Status.DynamicPassthroughDevices[0].Name = "secret-pci-inventory"
		vgpu := resolve("hardware.devices.vgpuDevices")
		Expect(vgpu.Available.VGPU.Values[0]).To(Equal(vmopv1.VirtualMachineClassResolvedVGPU{ProfileName: "gpu-a", DeviceVendorID: 123, FBSizeInGiB: 4, ProfileSharing: "timeSliced", ProfileClass: "compute"}))
		encoded, err := json.Marshal([]vmopv1.VirtualMachineClassResolvedField{vgpu, resolve("hardware.devices.dynamicDirectPathIODevices")})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).NotTo(ContainSubstring("secret-"))
		Expect(string(encoded)).NotTo(ContainSubstring("cluster-a"))
	})

	It("deduplicates and sorts equivalent identities deterministically", func() {
		first := resolve("hardware.devices.vgpuDevices")
		f.targets[0].Status.VGPUProfile = []vimv1.VirtualMachineVgpuProfileInfo{
			f.targets[0].Status.VGPUProfile[2], f.targets[0].Status.VGPUProfile[1], f.targets[0].Status.VGPUProfile[0], f.targets[0].Status.VGPUProfile[1],
		}
		Expect(resolve("hardware.devices.vgpuDevices")).To(Equal(first))
	})

	It("prefers discovered profile display data regardless of source cluster ordering", func() {
		second := f.targets[0].DeepCopy()
		second.UID, second.Spec.ID.ID = "b", "cluster-b"
		f.targets[0].Status.VGPUProfile = nil
		f.targets[0].Status.SharedGPUPassthroughTypes = []vimv1.VirtualMachinePciSharedGpuPassthroughInfo{{VGPU: "gpu-a"}}
		f.targets = append(f.targets, *second)
		f.ids = append(f.ids, "cluster-b")
		first := resolve("hardware.devices.vgpuDevices")
		Expect(first.Available.VGPU.Values).To(HaveLen(3))
		f.targets[0].Spec.ID.ID, f.targets[1].Spec.ID.ID = f.targets[1].Spec.ID.ID, f.targets[0].Spec.ID.ID
		Expect(resolve("hardware.devices.vgpuDevices")).To(Equal(first))
	})

	It("does not interpret an invalid matcher as a known empty hardware domain", func() {
		f.class.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Allowed: &[]vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Regex: ptr.To("[")}}}}
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Reason).To(Equal("InvalidDevicePolicy"))
		Expect(field.Available).To(BeNil())
	})

	It("does not expose choices under conflicting governing policies", func() {
		governor := f.class.DeepCopy()
		governor.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		governor.Name = "one"
		classes = append(classes, *governor)
		governor.Name = "two"
		classes = append(classes, *governor)
		field := resolve("hardware.devices.vgpuDevices")
		Expect(field.State).To(Equal("Unknown"))
		Expect(field.Reason).To(Equal("GovernanceUnresolved"))
		Expect(field.Available).To(BeNil())
		Expect(field.Policy).To(BeNil())
	})
})
