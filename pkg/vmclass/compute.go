// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"

func applyComputeDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, reset bool) {
	cpu := spec.CPUAdvanced
	if cpu == nil {
		cpu = &vmopv1.VirtualMachineClassCPUAdvancedSpec{}
	}
	memory := spec.MemoryAdvanced
	if memory == nil {
		memory = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{}
	}
	if vm.Spec.CPUAdvanced == nil {
		vm.Spec.CPUAdvanced = &vmopv1.VirtualMachineCPUAdvancedSpec{}
	}
	if vm.Spec.MemoryAdvanced == nil {
		vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{}
	}
	applyBoolDefault(&vm.Spec.CPUAdvanced.HotAddEnabled, cpu.HotAddEnabled, reset)
	applyBoolDefault(&vm.Spec.CPUAdvanced.HotRemoveEnabled, cpu.HotRemoveEnabled, reset)
	// New omissions defer existing flags for upgrade safety. A permitted
	// class change deliberately re-defaults this flag to disabled instead.
	if reset && vmopv1.ClassBoolDefault(cpu.HotRemoveEnabled) == nil {
		disabled := false
		vm.Spec.CPUAdvanced.HotRemoveEnabled = &disabled
	}
	applyBoolDefault(&vm.Spec.CPUAdvanced.IOMMUEnabled, cpu.IOMMUEnabled, reset)
	applyBoolDefault(&vm.Spec.CPUAdvanced.NestedHardwareVirtualizationEnabled, cpu.NestedHardwareVirtualizationEnabled, reset)
	applyBoolDefault(&vm.Spec.CPUAdvanced.PerformanceCountersEnabled, cpu.PerformanceCountersEnabled, reset)
	applyBoolDefault(&vm.Spec.CPUAdvanced.ExposeVNUMAOnCPUHotAdd, cpu.ExposeVNUMAOnCPUHotAdd, reset)
	applyBoolDefault(&vm.Spec.MemoryAdvanced.HotAddEnabled, memory.HotAddEnabled, reset)
	applyBoolDefault(&vm.Spec.MemoryAdvanced.ReservationLockedToMax, memory.ReservationLockedToMax, reset)
	// The lock expresses full reservation; the VM API forbids a separate
	// memory request while it is enabled. vSphere owns that reservation.
	if vm.Spec.MemoryAdvanced.ReservationLockedToMax != nil && *vm.Spec.MemoryAdvanced.ReservationLockedToMax && vm.Spec.Resources != nil && vm.Spec.Resources.Requests != nil {
		vm.Spec.Resources.Requests.Memory = nil
	}
	if value := vmopv1.ClassEnumDefault(cpu.LatencySensitivity); value != nil {
		level := vmopv1.VirtualMachineLatencySensitivityLevel(*value)
		vm.Spec.CPUAdvanced.LatencySensitivity = &level
	} else if reset {
		vm.Spec.CPUAdvanced.LatencySensitivity = nil
	}
	topology := cpu.Topology
	if topology == nil {
		topology = &vmopv1.VirtualMachineClassCPUTopologySpec{}
	}
	if vm.Spec.CPUAdvanced.Topology == nil {
		vm.Spec.CPUAdvanced.Topology = &vmopv1.VirtualMachineCPUTopologySpec{}
	}
	if value := vmopv1.ClassIntDefault(topology.CoresPerSocket); value != nil || reset {
		vm.Spec.CPUAdvanced.Topology.CoresPerSocket = value
	}
	if value := vmopv1.ClassIntDefault(topology.VNUMANodeCount); value != nil || reset {
		vm.Spec.CPUAdvanced.Topology.VNUMANodeCount = value
	}
	if vm.Spec.CPUAdvanced.Topology.CoresPerSocket == nil && vm.Spec.CPUAdvanced.Topology.VNUMANodeCount == nil {
		vm.Spec.CPUAdvanced.Topology = nil
	}
	if *vm.Spec.CPUAdvanced == (vmopv1.VirtualMachineCPUAdvancedSpec{}) {
		vm.Spec.CPUAdvanced = nil
	}
	if *vm.Spec.MemoryAdvanced == (vmopv1.VirtualMachineMemoryAdvancedSpec{}) {
		vm.Spec.MemoryAdvanced = nil
	}
}

// applyBoolDefault shares scalar materialization across class categories.
func applyBoolDefault(dst **bool, policy *vmopv1.BoolPolicy, reset bool) {
	if value := vmopv1.ClassBoolDefault(policy); value != nil || reset {
		*dst = value
	}
}
