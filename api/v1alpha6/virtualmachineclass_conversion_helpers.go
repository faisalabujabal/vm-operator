// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

// MaxClassDeviceExpansion bounds allocation when expanding counted devices.
// It is a conversion and configuration safety limit, not a hardware capability.
const MaxClassDeviceExpansion = 10000

// FixedClassResource converts a legacy scalar into a fixed class range.
func FixedClassResource(value resource.Quantity) *ResourceQuantityRange {
	minimum, maximum, def := value.DeepCopy(), value.DeepCopy(), value.DeepCopy()
	return &ResourceQuantityRange{Min: &minimum, Max: &maximum, Default: &def}
}

// ClassResourceDefault returns a class range's default for an older API.
func ClassResourceDefault(value *ResourceQuantityRange) resource.Quantity {
	if value == nil || value.Default == nil {
		return resource.Quantity{}
	}
	return value.Default.DeepCopy()
}

// FixedClassProfile converts a legacy reserved profile ID to a policy.
func FixedClassProfile(value string) *EnumPolicy {
	if value == "" {
		return nil
	}
	allowed, required := []string{value}, true
	return &EnumPolicy{Default: &value, Allowed: &allowed, Required: &required}
}

// ClassProfileDefault returns the selected class profile for an older API.
func ClassProfileDefault(value *EnumPolicy) string {
	if value == nil {
		return ""
	}
	if value.Default != nil {
		return *value.Default
	}
	if value.Allowed != nil && len(*value.Allowed) == 1 {
		return (*value.Allowed)[0]
	}
	return ""
}

// RestoreClassPolicies restores fields an older API cannot represent unless
// that API's writer changed their projected value. Projected is the preserved
// class converted down and back up without its conversion annotation.
func RestoreClassPolicies(dst, preserved, projected *VirtualMachineClassSpec) {
	for _, group := range []struct {
		dst, preserved, projected *VirtualMachineClassResourceSpec
	}{
		{&dst.Resources.Size, &preserved.Resources.Size, &projected.Resources.Size},
		{&dst.Resources.Requests, &preserved.Resources.Requests, &projected.Resources.Requests},
		{&dst.Resources.Limits, &preserved.Resources.Limits, &projected.Resources.Limits},
	} {
		if apiequality.Semantic.DeepEqual(group.dst.CPU, group.projected.CPU) {
			group.dst.CPU = group.preserved.CPU
		}
		if apiequality.Semantic.DeepEqual(group.dst.Memory, group.projected.Memory) {
			group.dst.Memory = group.preserved.Memory
		}
	}
	if apiequality.Semantic.DeepEqual(dst.ReservedProfileName, projected.ReservedProfileName) {
		dst.ReservedProfileName = preserved.ReservedProfileName
	}
	if apiequality.Semantic.DeepEqual(dst.Hardware.Devices.VGPUDevices, projected.Hardware.Devices.VGPUDevices) {
		dst.Hardware.Devices.VGPUDevices = preserved.Hardware.Devices.VGPUDevices
	}
	if apiequality.Semantic.DeepEqual(dst.Hardware.Devices.DynamicDirectPathIODevices, projected.Hardware.Devices.DynamicDirectPathIODevices) {
		dst.Hardware.Devices.DynamicDirectPathIODevices = preserved.Hardware.Devices.DynamicDirectPathIODevices
	}
}
