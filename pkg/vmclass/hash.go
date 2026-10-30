// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cespare/xxhash/v2"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
	vimtypes "github.com/vmware/govmomi/vim25/types"
)

// ConfigurationHash hashes typed configuration policies and the residual raw
// preset. Availability, governance scope, descriptive metadata and slot counts
// do not change VM configuration and therefore do not rotate class instances.
// Field ordering preserves the hashes produced by the incremental schema PRs.
func ConfigurationHash(ctx context.Context, spec *vmopv1.VirtualMachineClassSpec) (string, error) {
	var configSpec vimtypes.VirtualMachineConfigSpec
	if len(spec.ConfigSpec) > 0 {
		var err error
		configSpec, err = util.UnmarshalConfigSpecFromJSON(spec.ConfigSpec)
		if err != nil {
			return "", err
		}
		util.SanitizeVMClassConfigSpec(ctx, &configSpec)
	}

	data := struct {
		ConfigSpec          vimtypes.VirtualMachineConfigSpec             `json:"configSpec"`
		Hardware            vmopv1.VirtualMachineClassHardware            `json:"hardware"`
		Resources           vmopv1.VirtualMachineClassResources           `json:"resources"`
		Advanced            *vmopv1.VirtualMachineClassAdvancedSpec       `json:"advanced,omitempty"`
		ReservedProfileName *vmopv1.EnumPolicy                            `json:"reservedProfileName,omitempty"`
		CPUAdvanced         *vmopv1.VirtualMachineClassCPUAdvancedSpec    `json:"cpuAdvanced,omitempty"`
		MemoryAdvanced      *vmopv1.VirtualMachineClassMemoryAdvancedSpec `json:"memoryAdvanced,omitempty"`
		BootOptions         *vmopv1.VirtualMachineClassBootOptions        `json:"bootOptions,omitempty"`
	}{
		configSpec,
		spec.Hardware,
		spec.Resources,
		spec.Advanced,
		spec.ReservedProfileName,
		spec.CPUAdvanced,
		spec.MemoryAdvanced,
		spec.BootOptions,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal VM Class config to JSON: %w", err)
	}
	h := xxhash.New()
	if _, err := h.Write(b); err != nil {
		return "", fmt.Errorf("failed to write VM Class config to hash: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
