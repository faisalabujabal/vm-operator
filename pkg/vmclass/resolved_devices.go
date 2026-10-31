// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"cmp"
	"slices"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
)

// resolveDeviceFields reports identities, not instance inventory or capacity.
// Each class's matchers are evaluated independently so intersecting allow lists
// cannot accidentally become an OR of the two policies.
func resolveDeviceFields(class, ceiling *vmopv1.VirtualMachineClassSpec, clusters []resolvedCluster) []vmopv1.VirtualMachineClassResolvedField {
	fields, policies := resolvedDevicePolicies(class, ceiling)
	if len(clusters) == 0 {
		return fields
	}
	for _, cluster := range clusters {
		if cluster.target == nil {
			return fields
		}
	}
	// Validate policies even if discovery has no identities. CheckClass's list
	// evaluator validates every matcher before testing the candidate entries.
	empty := &vmopv1.VirtualMachineClassSpec{Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{
		VGPUDevices:                &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{}},
		DynamicDirectPathIODevices: &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &[]vmopv1.DynamicDirectPathIODevice{}},
	}}}
	for _, policy := range policies {
		if len(governance.CheckClass(empty, policy)) != 0 {
			for i := range fields {
				fields[i].Reason = "InvalidDevicePolicy"
			}
			return fields
		}
	}
	profiles, devices, directKnown := resolvedDeviceChoices(clusters, policies)
	// Stable ordering and duplicate elimination prevent status churn. Preserve
	// distinct display variants when a profile is supported by different models.
	slices.SortFunc(profiles, func(a, b vmopv1.VirtualMachineClassResolvedVGPU) int {
		return cmp.Or(cmp.Compare(a.ProfileName, b.ProfileName), cmp.Compare(a.DeviceVendorID, b.DeviceVendorID),
			cmp.Compare(a.FBSizeInGiB, b.FBSizeInGiB), cmp.Compare(a.ProfileSharing, b.ProfileSharing), cmp.Compare(a.ProfileClass, b.ProfileClass))
	})
	slices.SortFunc(devices, func(a, b vmopv1.VirtualMachineClassResolvedDirectPath) int {
		return cmp.Or(cmp.Compare(a.VendorID, b.VendorID), cmp.Compare(a.DeviceID, b.DeviceID), cmp.Compare(a.CustomLabel, b.CustomLabel),
			cmp.Compare(a.VendorName, b.VendorName), cmp.Compare(a.DeviceName, b.DeviceName))
	})
	fields[0].Available = &vmopv1.VirtualMachineClassResolvedDomain{VGPU: &vmopv1.VirtualMachineClassResolvedVGPUDomain{Values: slices.Compact(profiles)}}
	fields[1].Available = &vmopv1.VirtualMachineClassResolvedDomain{DirectPath: &vmopv1.VirtualMachineClassResolvedDirectPathDomain{Values: slices.Compact(devices)}}
	for i := range fields {
		fields[i].State, fields[i].Reason = "Complete", "DiscoveredDeviceIdentities"
	}
	if !directKnown {
		fields[1].State, fields[1].Reason, fields[1].Available = "Unknown", "InvalidDeviceDiscovery", nil
	}
	return fields
}

func resolvedDevicePolicies(class, ceiling *vmopv1.VirtualMachineClassSpec) ([]vmopv1.VirtualMachineClassResolvedField, []*vmopv1.VirtualMachineClassSpec) {
	vgpu := &vmopv1.VirtualMachineClassResolvedVGPUDomain{}
	direct := &vmopv1.VirtualMachineClassResolvedDirectPathDomain{}
	var policies []*vmopv1.VirtualMachineClassSpec
	for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
		if spec == nil {
			continue
		}
		// Defaults do not restrict the identity domain. Copy only policies so
		// no raw presets or unrelated scalar restrictions affect this field.
		p := &vmopv1.VirtualMachineClassSpec{}
		p.Hardware.Devices = *spec.Hardware.Devices.DeepCopy()
		policies = append(policies, p)
		if p := p.Hardware.Devices.VGPUDevices; p != nil {
			if p.Allowed != nil {
				vgpu.Allowed = append(vgpu.Allowed, vmopv1.VirtualMachineClassVGPUMatcherGroup{Matchers: resolvedVGPUMatchers(*p.Allowed)})
			}
			if p.Denied != nil {
				vgpu.Denied = append(vgpu.Denied, resolvedVGPUMatchers(*p.Denied)...)
			}
		}
		if p := p.Hardware.Devices.DynamicDirectPathIODevices; p != nil {
			if p.Allowed != nil {
				direct.Allowed = append(direct.Allowed, vmopv1.VirtualMachineClassDirectPathMatcherGroup{Matchers: resolvedDirectPathMatchers(*p.Allowed)})
			}
			if p.Denied != nil {
				direct.Denied = append(direct.Denied, resolvedDirectPathMatchers(*p.Denied)...)
			}
		}
	}
	fields := []vmopv1.VirtualMachineClassResolvedField{
		{Path: "hardware.devices.vgpuDevices", State: "Unknown", Reason: "DiscoveryIncomplete", Policy: &vmopv1.VirtualMachineClassResolvedDomain{VGPU: vgpu}},
		{Path: "hardware.devices.dynamicDirectPathIODevices", State: "Unknown", Reason: "DiscoveryIncomplete", Policy: &vmopv1.VirtualMachineClassResolvedDomain{DirectPath: direct}},
	}
	return fields, policies
}

func resolvedDeviceChoices(clusters []resolvedCluster, policies []*vmopv1.VirtualMachineClassSpec) ([]vmopv1.VirtualMachineClassResolvedVGPU, []vmopv1.VirtualMachineClassResolvedDirectPath, bool) {
	var profiles []vmopv1.VirtualMachineClassResolvedVGPU
	var legacyProfiles []string
	directKnown := true
	var devices []vmopv1.VirtualMachineClassResolvedDirectPath
	for _, cluster := range clusters {
		for _, profile := range cluster.target.Status.VGPUProfile {
			if profile.ProfileName != "" && permitsVGPU(profile.ProfileName, policies) {
				profiles = append(profiles, vmopv1.VirtualMachineClassResolvedVGPU{
					ProfileName: profile.ProfileName, DeviceVendorID: profile.DeviceVendorID,
					FBSizeInGiB: profile.FbSizeInGib, ProfileSharing: profile.ProfileSharing, ProfileClass: profile.ProfileClass,
				})
			}
		}
		for _, profile := range cluster.target.Status.SharedGPUPassthroughTypes {
			if profile.VGPU != "" && permitsVGPU(profile.VGPU, policies) {
				legacyProfiles = append(legacyProfiles, profile.VGPU)
			}
		}
		for _, device := range cluster.target.Status.DynamicPassthroughDevices {
			if device.VendorID < -32768 || device.VendorID > 65535 || device.DeviceID < -32768 || device.DeviceID > 65535 {
				directKnown = false
				continue
			}
			item := vmopv1.DynamicDirectPathIODevice{VendorID: int64(device.VendorID) & 0xffff, DeviceID: int64(device.DeviceID) & 0xffff, CustomLabel: device.CustomLabel}
			if permitsDirectPath(item, policies) {
				devices = append(devices, vmopv1.VirtualMachineClassResolvedDirectPath{
					VendorID: item.VendorID, DeviceID: item.DeviceID, CustomLabel: item.CustomLabel,
					VendorName: device.VendorName, DeviceName: device.DeviceName,
				})
			}
		}
	}
	for _, name := range legacyProfiles {
		if !slices.ContainsFunc(profiles, func(p vmopv1.VirtualMachineClassResolvedVGPU) bool { return p.ProfileName == name }) {
			profiles = append(profiles, vmopv1.VirtualMachineClassResolvedVGPU{ProfileName: name})
		}
	}
	return profiles, devices, directKnown
}

func permitsVGPU(name string, policies []*vmopv1.VirtualMachineClassSpec) bool {
	candidate := &vmopv1.VirtualMachineClassSpec{Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{
		VGPUDevices: &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: name}}},
	}}}
	for _, policy := range policies {
		if len(governance.CheckClass(candidate, policy)) != 0 {
			return false
		}
	}
	return true
}

func permitsDirectPath(item vmopv1.DynamicDirectPathIODevice, policies []*vmopv1.VirtualMachineClassSpec) bool {
	candidate := &vmopv1.VirtualMachineClassSpec{Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{
		DynamicDirectPathIODevices: &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &[]vmopv1.DynamicDirectPathIODevice{item}},
	}}}
	for _, policy := range policies {
		if len(governance.CheckClass(candidate, policy)) != 0 {
			return false
		}
	}
	return true
}

func resolvedVGPUMatchers(matchers []vmopv1.VGPUDeviceMatcher) []vmopv1.VirtualMachineClassResolvedVGPUMatcher {
	result := make([]vmopv1.VirtualMachineClassResolvedVGPUMatcher, len(matchers))
	for i, m := range matchers {
		result[i].ProfileName = resolvedStringMatcher(m.ProfileName)
	}
	return result
}

func resolvedDirectPathMatchers(matchers []vmopv1.DynamicDirectPathIODeviceMatcher) []vmopv1.VirtualMachineClassResolvedDirectPathMatcher {
	result := make([]vmopv1.VirtualMachineClassResolvedDirectPathMatcher, len(matchers))
	for i, m := range matchers {
		isolated := m.DeepCopy()
		result[i] = vmopv1.VirtualMachineClassResolvedDirectPathMatcher{VendorID: isolated.VendorID, DeviceID: isolated.DeviceID, CustomLabel: resolvedStringMatcher(isolated.CustomLabel)}
	}
	return result
}
