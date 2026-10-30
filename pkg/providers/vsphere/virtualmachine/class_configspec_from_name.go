// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"
	"fmt"

	vimtypes "github.com/vmware/govmomi/vim25/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
)

// ClassConfigSpecFromClassSpec returns the vim ConfigSpec derived from a
// VirtualMachineClassSpec, projecting typed presets through the same mapping
// used by older API conversions while preserving legacy device precedence.
func ClassConfigSpecFromClassSpec(
	ctx context.Context,
	spec *vmopv1.VirtualMachineClassSpec,
) (vimtypes.VirtualMachineConfigSpec, error) {

	if spec == nil {
		return vimtypes.VirtualMachineConfigSpec{}, nil
	}

	raw, err := vmopv1.ProjectClassConfigSpec(spec)
	if err != nil {
		return vimtypes.VirtualMachineConfigSpec{}, fmt.Errorf("project class presets: %w", err)
	}
	if len(raw) > 0 {
		configSpec, err := util.UnmarshalConfigSpecFromJSON(raw)
		if err != nil {
			return vimtypes.VirtualMachineConfigSpec{}, err
		}
		util.SanitizeVMClassConfigSpec(ctx, &configSpec)
		// Projecting a typed preset creates ConfigSpec JSON even when the class
		// had none. Its typed devices still apply in that case.
		if len(spec.ConfigSpec) == 0 {
			devices, err := ConfigSpecFromVMClassDevices(spec)
			if err != nil {
				return vimtypes.VirtualMachineConfigSpec{}, fmt.Errorf("project class devices: %w", err)
			}
			configSpec.DeviceChange = append(configSpec.DeviceChange, devices.DeviceChange...)
		}
		return configSpec, nil
	}

	return ConfigSpecFromVMClassDevices(spec)
}

// ClassConfigSpecForVirtualMachine returns the ConfigSpec used to derive NIC
// device types and similar class-backed defaults. When spec.class names a
// VirtualMachineClassInstance, that instance's embedded class snapshot is used;
// otherwise the named VirtualMachineClass is loaded.
func ClassConfigSpecForVirtualMachine(
	ctx context.Context,
	c ctrlclient.Client,
	vm *vmopv1.VirtualMachine,
) (vimtypes.VirtualMachineConfigSpec, error) {

	if vm.Spec.Class != nil && vm.Spec.Class.Name != "" {
		var inst vmopv1.VirtualMachineClassInstance
		key := ctrlclient.ObjectKey{Name: vm.Spec.Class.Name, Namespace: vm.Namespace}
		if err := c.Get(ctx, key, &inst); err != nil {
			return vimtypes.VirtualMachineConfigSpec{}, fmt.Errorf(
				"failed to get VirtualMachineClassInstance %s: %w",
				vm.Spec.Class.Name, err)
		}
		return ClassConfigSpecFromClassSpec(ctx, &inst.Spec.VirtualMachineClassSpec)
	}

	return ClassConfigSpecForName(ctx, c, vm.Spec.ClassName, vm.Namespace)
}

// ClassConfigSpecForName returns the ConfigSpec derived from the named
// VirtualMachineClass in the namespace, from its ConfigSpec JSON or hardware
// devices.
func ClassConfigSpecForName(
	ctx context.Context,
	c ctrlclient.Client,
	className, namespace string,
) (vimtypes.VirtualMachineConfigSpec, error) {

	if className == "" {
		return vimtypes.VirtualMachineConfigSpec{}, nil
	}

	vmClass := &vmopv1.VirtualMachineClass{}
	key := ctrlclient.ObjectKey{Name: className, Namespace: namespace}
	if err := c.Get(ctx, key, vmClass); err != nil {
		return vimtypes.VirtualMachineConfigSpec{}, fmt.Errorf(
			"failed to get VM Class %s: %w", className, err)
	}

	return ClassConfigSpecFromClassSpec(ctx, &vmClass.Spec)
}
