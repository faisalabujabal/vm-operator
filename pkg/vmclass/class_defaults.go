// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"fmt"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
)

// ApplyClassDefaults defaults governing-class enforcement and fixes the memory
// reservation lock on presets that attach passthrough devices. The caller must
// gate this behavior on VMServiceClasslessVM. Resource ranges are unchanged.
func ApplyClassDefaults(spec *vmopv1.VirtualMachineClassSpec) (bool, error) {
	requiresLock, err := classRequiresMemoryReservationLock(spec)
	if err != nil {
		return false, err
	}
	changed := false
	if spec.Governs != nil && spec.Governs.Enforcement == "" {
		spec.Governs.Enforcement = vmopv1.VirtualMachineClassEnforcementDeny
		changed = true
	}
	if requiresLock {
		if spec.MemoryAdvanced == nil {
			spec.MemoryAdvanced = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{}
		}
		policy := spec.MemoryAdvanced.ReservationLockedToMax
		if policy == nil || policy.Default == nil || !*policy.Default ||
			policy.AllowOverride == nil || *policy.AllowOverride {
			spec.MemoryAdvanced.ReservationLockedToMax = vmopv1.FixedClassBool(true)
			changed = true
		}
	}
	return changed, nil
}

func classRequiresMemoryReservationLock(spec *vmopv1.VirtualMachineClassSpec) (bool, error) {
	// Match the provider's legacy precedence: any raw ConfigSpec owns device
	// configuration, even when its device list is empty. Typed defaults are
	// used only when that raw preset is absent.
	if len(spec.ConfigSpec) > 0 {
		configSpec, err := util.UnmarshalConfigSpecFromJSON(spec.ConfigSpec)
		if err != nil {
			return false, fmt.Errorf("read class device presets: %w", err)
		}
		for i, change := range configSpec.DeviceChange {
			if change == nil {
				return false, fmt.Errorf("read class device presets: deviceChange[%d] must not be null", i)
			}
		}
		return util.RequiresMemoryReservationLock(nil, configSpec.DeviceChange), nil
	}
	devices := spec.Hardware.Devices
	return len(ListDefaults(devices.VGPUDevices)) > 0 ||
		len(ListDefaults(devices.DynamicDirectPathIODevices)) > 0, nil
}
