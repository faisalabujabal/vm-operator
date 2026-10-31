// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
	vimtypes "github.com/vmware/govmomi/vim25/types"
)

// DeviceMemoryReservationSatisfied checks desired and still-attached passthrough
// devices against effective guest memory intent. It never changes its inputs.
// Admission callers handle capability and request-identity exemptions.
func DeviceMemoryReservationSatisfied(ctx context.Context, c ctrlclient.Client, vm, oldVM *vmopv1.VirtualMachine) (bool, error) {
	if vmopv1util.FullMemReservationSpecMet(vm) {
		return true, nil
	}
	hasDevices := hasPassthroughDevices(vm)
	if oldVM != nil {
		hasDevices = hasDevices || hasObservedPassthroughDevices(oldVM)
	}
	var spec *vmopv1.VirtualMachineClassSpec
	if vm.Spec.ClassName != "" {
		var err error
		spec, err = resolveClassSpec(ctx, c, vm, oldVM)
		if err != nil {
			return false, err
		}
	}
	effective := vm.DeepCopy()
	if spec != nil {
		raw, err := vmopv1.ProjectClassConfigSpec(spec)
		if err != nil {
			return false, fmt.Errorf("resolve device class configuration: %w", err)
		}
		var config vimtypes.VirtualMachineConfigSpec
		if len(raw) > 0 {
			config, err = util.UnmarshalConfigSpecFromJSON(raw)
			if err != nil {
				return false, fmt.Errorf("resolve device class configuration: %w", err)
			}
		}
		hasDevices = hasDevices || len(ListDefaults(spec.Hardware.Devices.VGPUDevices)) > 0 || len(ListDefaults(spec.Hardware.Devices.DynamicDirectPathIODevices)) > 0
		for _, change := range config.DeviceChange {
			if change == nil {
				continue
			}
			device := change.GetVirtualDeviceConfigSpec()
			if device.Operation == vimtypes.VirtualDeviceConfigSpecOperationRemove {
				continue
			}
			if _, ok := device.Device.(*vimtypes.VirtualPCIPassthrough); ok {
				hasDevices = true
			}
		}
		if !hasDevices {
			return true, nil
		}
		// Match ApplyDefaults: ordinary updates retain materialized VM intent.
		// An omitted request/lock on that path means zero/false, not inheritance.
		if oldVM == nil || (pkgcfg.FromContext(ctx).Features.VMResizeCPUMemory &&
			(vm.Spec.ClassName != oldVM.Spec.ClassName || !apiequality.Semantic.DeepEqual(vm.Spec.Class, oldVM.Spec.Class))) {
			fillDeviceMemoryDefaults(effective, spec, config)
		}
	}
	return !hasDevices || vmopv1util.FullMemReservationSpecMet(effective), nil
}

func hasPassthroughDevices(vm *vmopv1.VirtualMachine) bool {
	if vm.Spec.Hardware != nil && vm.Spec.Hardware.Devices != nil {
		d := vm.Spec.Hardware.Devices
		if len(d.VGPUDevices) > 0 || len(d.DynamicDirectPathIODevices) > 0 {
			return true
		}
	}
	return hasObservedPassthroughDevices(vm)
}

func hasObservedPassthroughDevices(vm *vmopv1.VirtualMachine) bool {
	return vm.Status.Hardware != nil && (len(vm.Status.Hardware.VGPUs) > 0 || len(vm.Status.Hardware.DynamicDirectPathIODevices) > 0)
}

// fillDeviceMemoryDefaults resolves only missing VM memory fields. Typed class
// defaults override raw presets, matching configuration materialization. No CPU
// frequency or vSphere query is needed for this memory-only admission rule.
func fillDeviceMemoryDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, config vimtypes.VirtualMachineConfigSpec) {
	if vm.Spec.MemoryAdvanced == nil {
		vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{}
	}
	if vm.Spec.MemoryAdvanced.ReservationLockedToMax == nil {
		vm.Spec.MemoryAdvanced.ReservationLockedToMax = config.MemoryReservationLockedToMax
	}
	if vm.Spec.Resources == nil {
		vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{}
	}
	if vm.Spec.Resources.Size == nil {
		vm.Spec.Resources.Size = &vmopv1.VirtualMachineResourceQuantity{}
	}
	if vm.Spec.Resources.Requests == nil {
		vm.Spec.Resources.Requests = &vmopv1.VirtualMachineResourceQuantity{}
	}
	if vm.Spec.Resources.Size.Memory == nil {
		if p := spec.Resources.Size.Memory; p != nil && p.Default != nil {
			q := p.Default.DeepCopy()
			vm.Spec.Resources.Size.Memory = &q
		} else if config.MemoryMB > 0 {
			vm.Spec.Resources.Size.Memory = resource.NewQuantity(config.MemoryMB*1024*1024, resource.BinarySI)
		}
	}
	if vm.Spec.Resources.Requests.Memory == nil {
		request, limit := spec.Resources.Requests.Memory, spec.Resources.Limits.Memory
		// A nonzero typed allocation replaces the raw allocation as a whole.
		switch {
		case request != nil && request.Default != nil && !request.Default.IsZero():
			q := request.Default.DeepCopy()
			vm.Spec.Resources.Requests.Memory = &q
		case limit != nil && limit.Default != nil && !limit.Default.IsZero():
			vm.Spec.Resources.Requests.Memory = resource.NewQuantity(0, resource.BinarySI)
		case config.MemoryAllocation != nil && config.MemoryAllocation.Reservation != nil:
			vm.Spec.Resources.Requests.Memory = resource.NewQuantity(*config.MemoryAllocation.Reservation*1024*1024, resource.BinarySI)
		}
	}
}
