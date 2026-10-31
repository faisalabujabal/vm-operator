// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	vimtypes "github.com/vmware/govmomi/vim25/types"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
)

// HardwareCompatibility evaluates the class defaults against each cluster in a
// zone. At least one cluster must support all checked defaults; evidence from
// different clusters is never combined. This is a hardware support check, not a
// placement or capacity guarantee. Policy ranges are resolved separately.
// Guest-specific checks apply only when the class supplies a guest ID; otherwise
// the VM image determines guest support later.
func HardwareCompatibility(class *vmopv1.VirtualMachineClass, clusterIDs []string,
	targets []vimv1.ConfigTarget, options []vimv1.VirtualMachineConfigOptions,
	guests []vimv1.VirtualMachineGuestOptions) metav1.Condition {
	result := metav1.Condition{Type: vmopv1.VirtualMachineClassConditionHardwareCompatible,
		ObservedGeneration: class.Generation, Status: metav1.ConditionUnknown,
		Reason: vmopv1.VirtualMachineClassHardwareUnknownReason, Message: "Cluster-specific hardware discovery is incomplete"}
	spec, raw, err := resolveHardwareDefaults(&class.Spec)
	if err != nil {
		result.Message = "Class defaults could not be resolved"
		return result
	}
	version := raw.Version
	unknown := len(clusterIDs) == 0
	var failures []string
	for _, id := range clusterIDs {
		var target *vimv1.ConfigTarget
		for i := range targets {
			if targets[i].Spec.ID.ID == id {
				if target != nil { // Ambiguous discovery is not authoritative.
					target = nil
					break
				}
				target = &targets[i]
			}
		}
		if target == nil || !discoveryReady(target, target.Status.ObservedGeneration, target.Status.Conditions) {
			unknown = true
			continue
		}
		v := version
		if v == "" {
			v = target.Status.MaxHardwareVersion
		}
		if version != "" {
			requested, reqErr := strconv.Atoi(strings.TrimPrefix(version, "vmx-"))
			maximum, maxErr := strconv.Atoi(strings.TrimPrefix(target.Status.MaxHardwareVersion, "vmx-"))
			if reqErr != nil || maxErr != nil {
				unknown = true
				continue
			}
			if requested > maximum {
				failures = append(failures, "configSpec.version")
				continue
			}
		}
		check := hardwareCheck{}
		check.target(spec, &target.Status)
		if len(class.Spec.ConfigSpec) > 0 {
			check.rawDevices(raw.DeviceChange, &target.Status)
		}
		if needsHardwareOptions(spec, raw.GuestId) && len(check.failures) == 0 {
			var option *vimv1.VirtualMachineConfigOptions
			for i := range options {
				if options[i].Spec.HardwareVersion == v && ownedBy(&options[i], target) &&
					options[i].Annotations["vmoperator.vmware.com/environment-browser-cluster"] == id {
					option = &options[i]
					break
				}
			}
			// Shared options contain a snapshot from only their annotated source
			// cluster. Ownership alone cannot prove support in a different cluster.
			if v == "" || option == nil || !discoveryReady(option, option.Status.ObservedGeneration, option.Status.Conditions) {
				check.unknown = true
			} else {
				check.options(spec, &option.Status)
				check.guestOptions(spec, raw.GuestId, raw.Firmware, v, option, guests)
			}
		}

		switch {
		case len(check.failures) != 0:
			failures = append(failures, check.failures...)
		case check.unknown:
			unknown = true
		default:
			result.Status = metav1.ConditionTrue
			result.Reason = vmopv1.VirtualMachineClassHardwareSupportedReason
			result.Message = "Discovered hardware supports the class defaults"
			return result
		}
	}
	if !unknown {
		slices.Sort(failures)
		result.Status = metav1.ConditionFalse
		result.Reason = vmopv1.VirtualMachineClassHardwareUnsupportedReason
		// Only class field paths are exposed, never inventory or pool totals.
		result.Message = "No cluster supports all class defaults; unsupported fields: " + strings.Join(slices.Compact(failures), ", ")
	}
	return result
}

func discoveryReady(obj metav1.Object, observed int64, conditions []metav1.Condition) bool {
	return obj.GetDeletionTimestamp().IsZero() && observed == obj.GetGeneration() &&
		apimeta.IsStatusConditionTrue(conditions, vimv1.ReadyConditionType)
}

func ownedBy(obj, owner metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID != "" && ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

type hardwareCheck struct {
	unknown  bool
	failures []string
}

func (c *hardwareCheck) require(field string, supported bool) {
	if !supported {
		c.failures = append(c.failures, field)
	}
}

func enabled(p *vmopv1.BoolPolicy) bool {
	v := vmopv1.ClassBoolDefault(p)
	return v != nil && *v
}

func (c *hardwareCheck) target(spec *vmopv1.VirtualMachineClassSpec, target *vimv1.ConfigTargetStatus) {
	if p := spec.Resources.Size.CPU; p != nil && p.Default != nil && p.Default.Sign() > 0 {
		if target.MaxCPUsPerVM <= 0 {
			c.unknown = true
		} else {
			c.require("resources.size.cpu", p.Default.Value() <= int64(target.MaxCPUsPerVM))
		}
	}
	if p := spec.Resources.Size.Memory; p != nil && p.Default != nil && p.Default.Sign() > 0 {
		if target.SupportedMaxMem == nil || target.SupportedMaxMem.Sign() <= 0 {
			c.unknown = true
		} else {
			c.require("resources.size.memory", p.Default.Cmp(*target.SupportedMaxMem) <= 0)
		}
	}
	for _, requested := range ListDefaults(spec.Hardware.Devices.VGPUDevices) {
		supported := slices.ContainsFunc(target.VGPUProfile, func(p vimv1.VirtualMachineVgpuProfileInfo) bool { return p.ProfileName == requested.ProfileName }) ||
			slices.ContainsFunc(target.SharedGPUPassthroughTypes, func(p vimv1.VirtualMachinePciSharedGpuPassthroughInfo) bool { return p.VGPU == requested.ProfileName })
		c.require("hardware.devices.vgpuDevices", supported)
	}
	for _, requested := range ListDefaults(spec.Hardware.Devices.DynamicDirectPathIODevices) {
		c.require("hardware.devices.dynamicDirectPathIODevices", slices.ContainsFunc(target.DynamicPassthroughDevices, func(p vimv1.VirtualMachineDynamicPassthroughInfo) bool {
			return (int64(p.VendorID)&0xffff) == requested.VendorID && (int64(p.DeviceID)&0xffff) == requested.DeviceID &&
				(requested.CustomLabel == "" || requested.CustomLabel == p.CustomLabel)
		}))
	}
}

func (c *hardwareCheck) options(spec *vmopv1.VirtualMachineClassSpec, options *vimv1.VirtualMachineConfigOptionsStatus) {
	hw := options.HardwareOptions
	if hw == nil {
		c.unknown = true
	} else {
		if p := spec.Resources.Size.CPU; p != nil && p.Default != nil && p.Default.Sign() > 0 {
			if len(hw.NumCPU) == 0 {
				c.unknown = true
			} else {
				c.require("resources.size.cpu", slices.ContainsFunc(hw.NumCPU, func(n int32) bool { return int64(n) == p.Default.Value() }))
			}
		}
		if p := spec.Resources.Size.Memory; p != nil && p.Default != nil && p.Default.Sign() > 0 {
			if hw.Memory == nil {
				c.unknown = true
			} else {
				c.require("resources.size.memory", p.Default.Cmp(hw.Memory.Min) >= 0 && p.Default.Cmp(hw.Memory.Max) <= 0)
			}
		}
		if cpu := spec.CPUAdvanced; cpu != nil && cpu.Topology != nil {
			c.intOption("cpuAdvanced.topology.coresPerSocket", cpu.Topology.CoresPerSocket, hw.NumCoresPerSocket)
			c.intOption("cpuAdvanced.topology.vnumaNodeCount", cpu.Topology.VNUMANodeCount, hw.NumNumaNodes)
		}
	}
	capabilities := options.Capabilities
	if capabilities == nil {
		c.unknown = true
		return
	}
	if cpu := spec.CPUAdvanced; cpu != nil {
		if enabled(cpu.NestedHardwareVirtualizationEnabled) {
			c.require("cpuAdvanced.nestedHardwareVirtualizationEnabled", capabilities.NestedHVSupported)
		}
		if enabled(cpu.PerformanceCountersEnabled) {
			c.require("cpuAdvanced.performanceCountersEnabled", capabilities.VPMCSupported)
		}
	}
	if mem := spec.MemoryAdvanced; mem != nil && enabled(mem.ReservationLockedToMax) {
		c.require("memoryAdvanced.reservationLockedToMax", capabilities.MemoryReservationLockSupported)
	}
	if boot := spec.BootOptions; boot != nil {
		if enabled(boot.BootRetry) {
			c.require("bootOptions.bootRetry", capabilities.BootRetryOptionsSupported)
		}
		if enabled(boot.EFISecureBoot) {
			if capabilities.SecureBootSupported == nil {
				c.unknown = true
			} else {
				c.require("bootOptions.efiSecureBoot", *capabilities.SecureBootSupported)
			}
		}
	}
	if advanced := spec.Advanced; advanced != nil && enabled(advanced.ChangeBlockTracking) {
		c.require("advanced.changeBlockTracking", capabilities.ChangeTrackingSupported)
	}
}

func (c *hardwareCheck) intOption(field string, policy *vmopv1.IntRange, option *vimv1.IntOption) {
	v := vmopv1.ClassIntDefault(policy)
	if v == nil || *v == 0 {
		return
	} // Zero selects automatic topology.
	if option == nil {
		c.unknown = true
		return
	}
	c.require(field, *v >= option.Min && *v <= option.Max)
}

func (c *hardwareCheck) guest(spec *vmopv1.VirtualMachineClassSpec, firmware string, guest *vimv1.VirtualMachineGuestOptionsHardwareVersionStatus) {
	if p := spec.Resources.Size.CPU; p != nil && p.Default != nil && p.Default.Sign() > 0 {
		if guest.SupportedMaxCPUs <= 0 {
			c.unknown = true
		} else {
			c.require("resources.size.cpu", p.Default.Value() <= int64(guest.SupportedMaxCPUs))
		}
	}
	if p := spec.Resources.Size.Memory; p != nil && p.Default != nil && p.Default.Sign() > 0 {
		if guest.SupportedMaxMem == nil || guest.SupportedMinMem == nil {
			c.unknown = true
		} else {
			c.require("resources.size.memory", p.Default.Cmp(*guest.SupportedMinMem) >= 0 && p.Default.Cmp(*guest.SupportedMaxMem) <= 0)
		}
	}
	if firmware != "" {
		c.require("configSpec.firmware", slices.Contains(guest.SupportedFirmware, vimv1.Firmware(strings.ToUpper(firmware))))
	}
	if cpu := spec.CPUAdvanced; cpu != nil {
		if enabled(cpu.HotAddEnabled) {
			c.require("cpuAdvanced.hotAddEnabled", guest.SupportsCpuHotAdd)
		}
		if enabled(cpu.HotRemoveEnabled) {
			c.require("cpuAdvanced.hotRemoveEnabled", guest.SupportsCpuHotRemove)
		}
		if enabled(cpu.IOMMUEnabled) {
			if guest.VvtdSupported == nil {
				c.unknown = true
			} else {
				c.require("cpuAdvanced.iommuEnabled", guest.VvtdSupported.Supported)
			}
		}
	}
	if mem := spec.MemoryAdvanced; mem != nil && enabled(mem.HotAddEnabled) {
		c.require("memoryAdvanced.hotAddEnabled", guest.SupportsMemoryHotAdd)
	}
	if boot := spec.BootOptions; boot != nil && enabled(boot.EFISecureBoot) {
		if guest.SupportsSecureBoot == nil {
			c.unknown = true
		} else {
			c.require("bootOptions.efiSecureBoot", *guest.SupportsSecureBoot)
		}
	}
}

// rawDevices follows legacy ConfigSpec device precedence. Dynamic backing entries
// describe alternatives for one device, not a request for every alternative.
func (c *hardwareCheck) rawDevices(changes []vimtypes.BaseVirtualDeviceConfigSpec, target *vimv1.ConfigTargetStatus) {
	for _, change := range changes {
		if change == nil {
			continue
		}
		dc := change.GetVirtualDeviceConfigSpec()
		if dc.Operation == vimtypes.VirtualDeviceConfigSpecOperationRemove {
			continue
		}
		pci, ok := dc.Device.(*vimtypes.VirtualPCIPassthrough)
		if !ok {
			continue
		}
		switch backing := pci.Backing.(type) {
		case *vimtypes.VirtualPCIPassthroughVmiopBackingInfo:
			c.require("configSpec.deviceChange", slices.ContainsFunc(target.VGPUProfile, func(p vimv1.VirtualMachineVgpuProfileInfo) bool { return p.ProfileName == backing.Vgpu }) ||
				slices.ContainsFunc(target.SharedGPUPassthroughTypes, func(p vimv1.VirtualMachinePciSharedGpuPassthroughInfo) bool { return p.VGPU == backing.Vgpu }))
		case *vimtypes.VirtualPCIPassthroughDynamicBackingInfo:
			c.dynamicBacking(backing, target.DynamicPassthroughDevices)

		default:
			c.unknown = true // Static passthrough needs host-specific placement data.
		}
	}
}

// resolveHardwareDefaults shares typed/raw precedence with VM provisioning.
func resolveHardwareDefaults(input *vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineClassSpec, vimtypes.VirtualMachineConfigSpec, error) {
	spec := input.DeepCopy()
	rawJSON, err := vmopv1.ProjectClassConfigSpec(input)
	if err != nil {
		return nil, vimtypes.VirtualMachineConfigSpec{}, err
	}
	spec.ConfigSpec = rawJSON
	var raw vimtypes.VirtualMachineConfigSpec
	if len(rawJSON) > 0 {
		raw, err = util.UnmarshalConfigSpecFromJSON(rawJSON)
		if err != nil {
			return nil, raw, err
		}
	}
	if err := vmopv1.PromoteClassConfigSpec(spec); err != nil {
		return nil, raw, err
	}
	if p := spec.Resources.Size.CPU; (p == nil || p.Default == nil) && raw.NumCPUs > 0 {
		spec.Resources.Size.CPU = vmopv1.FixedClassResource(*resource.NewQuantity(int64(raw.NumCPUs), resource.DecimalSI))
	}
	if p := spec.Resources.Size.Memory; (p == nil || p.Default == nil) && raw.MemoryMB > 0 {
		spec.Resources.Size.Memory = vmopv1.FixedClassResource(resource.MustParse(fmt.Sprintf("%dMi", raw.MemoryMB)))
	}
	// An original ConfigSpec takes precedence over typed devices in the provider.
	if len(input.ConfigSpec) > 0 {
		spec.Hardware.Devices = vmopv1.VirtualDevices{}
	}
	return spec, raw, nil
}

func (c *hardwareCheck) guestOptions(spec *vmopv1.VirtualMachineClassSpec, guestID, firmware, version string,
	option *vimv1.VirtualMachineConfigOptions, guests []vimv1.VirtualMachineGuestOptions) {
	if guestID == "" {
		return
	}
	// Guest objects are shared, guest-ID-keyed and have no owner references.
	// The ready source options must still advertise this guest for this version.
	if !slices.ContainsFunc(option.Status.GuestOSIdentifiers, func(id vimv1.VirtualMachineGuestOSIdentifier) bool {
		return string(id) == guestID || id.ToVimType() == guestID
	}) {
		c.require("configSpec.guestId", false)
		return
	}
	for i := range guests {
		if (string(guests[i].Spec.ID) != guestID && guests[i].Spec.ID.ToVimType() != guestID) || !guests[i].DeletionTimestamp.IsZero() {
			continue
		}
		for j := range guests[i].Status.HardwareVersions {
			if guests[i].Status.HardwareVersions[j].HardwareVersion == version {
				c.guest(spec, firmware, &guests[i].Status.HardwareVersions[j])
				return
			}
		}
	}
	c.unknown = true
}

// needsHardwareOptions avoids treating unrelated cluster snapshots as evidence.
// Device-only classes can be decided entirely from their own ConfigTarget.
func needsHardwareOptions(spec *vmopv1.VirtualMachineClassSpec, guestID string) bool {
	for _, p := range []*vmopv1.ResourceQuantityRange{spec.Resources.Size.CPU, spec.Resources.Size.Memory} {
		if p != nil && p.Default != nil && p.Default.Sign() > 0 {
			return true
		}
	}
	if cpu := spec.CPUAdvanced; cpu != nil {
		if enabled(cpu.NestedHardwareVirtualizationEnabled) || enabled(cpu.PerformanceCountersEnabled) {
			return true
		}
		if cpu.Topology != nil {
			for _, p := range []*vmopv1.IntRange{cpu.Topology.CoresPerSocket, cpu.Topology.VNUMANodeCount} {
				if v := vmopv1.ClassIntDefault(p); v != nil && *v > 0 {
					return true
				}
			}
		}
	}
	return guestID != "" ||
		(spec.MemoryAdvanced != nil && enabled(spec.MemoryAdvanced.ReservationLockedToMax)) ||
		(spec.BootOptions != nil && (enabled(spec.BootOptions.BootRetry) || enabled(spec.BootOptions.EFISecureBoot))) ||
		(spec.Advanced != nil && enabled(spec.Advanced.ChangeBlockTracking))
}

func (c *hardwareCheck) dynamicBacking(backing *vimtypes.VirtualPCIPassthroughDynamicBackingInfo, devices []vimv1.VirtualMachineDynamicPassthroughInfo) {
	unknown := len(backing.AllowedDevice) == 0
	for _, p := range devices {
		if backing.CustomLabel != "" && backing.CustomLabel != p.CustomLabel {
			continue
		}
		for _, d := range backing.AllowedDevice {
			// vSphere may encode PCI IDs as signed 16-bit integers in int32 fields.
			if (int64(d.VendorId)&0xffff) != (int64(p.VendorID)&0xffff) || (int64(d.DeviceId)&0xffff) != (int64(p.DeviceID)&0xffff) {
				continue
			}
			// Discovery does not publish these additional identity constraints.
			// Another unconstrained alternative can still establish support.
			if d.SubVendorId != 0 || d.SubDeviceId != 0 || d.RevisionId != 0 {
				unknown = true
				continue
			}
			return
		}
	}
	if unknown {
		c.unknown = true
	} else {
		c.require("configSpec.deviceChange", false)
	}
}
