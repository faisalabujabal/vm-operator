// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"

func applyComputeDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, reset bool) {
	cpu := spec.CPUAdvanced
	if cpu == nil { cpu = &vmopv1.VirtualMachineClassCPUAdvancedSpec{} }
	memory := spec.MemoryAdvanced
	if memory == nil { memory = &vmopv1.VirtualMachineClassMemoryAdvancedSpec{} }
	if vm.Spec.CPUAdvanced == nil { vm.Spec.CPUAdvanced = &vmopv1.VirtualMachineCPUAdvancedSpec{} }
	if vm.Spec.MemoryAdvanced == nil { vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{} }
	if value := vmopv1.ClassBoolDefault(cpu.HotAddEnabled); value != nil || reset { vm.Spec.CPUAdvanced.HotAddEnabled = value }
	if value := vmopv1.ClassBoolDefault(cpu.HotRemoveEnabled); value != nil || reset { vm.Spec.CPUAdvanced.HotRemoveEnabled = value }
	if value := vmopv1.ClassBoolDefault(cpu.IOMMUEnabled); value != nil || reset { vm.Spec.CPUAdvanced.IOMMUEnabled = value }
	if value := vmopv1.ClassBoolDefault(cpu.NestedHardwareVirtualizationEnabled); value != nil || reset { vm.Spec.CPUAdvanced.NestedHardwareVirtualizationEnabled = value }
	if value := vmopv1.ClassBoolDefault(cpu.PerformanceCountersEnabled); value != nil || reset { vm.Spec.CPUAdvanced.PerformanceCountersEnabled = value }
	if value := vmopv1.ClassBoolDefault(cpu.ExposeVNUMAOnCPUHotAdd); value != nil || reset { vm.Spec.CPUAdvanced.ExposeVNUMAOnCPUHotAdd = value }
	if value := vmopv1.ClassBoolDefault(memory.HotAddEnabled); value != nil || reset { vm.Spec.MemoryAdvanced.HotAddEnabled = value }
	if value := vmopv1.ClassBoolDefault(memory.ReservationLockedToMax); value != nil || reset { vm.Spec.MemoryAdvanced.ReservationLockedToMax = value }
	if value := vmopv1.ClassEnumDefault(cpu.LatencySensitivity); value != nil {
		level := vmopv1.VirtualMachineLatencySensitivityLevel(*value); vm.Spec.CPUAdvanced.LatencySensitivity = &level
	} else if reset { vm.Spec.CPUAdvanced.LatencySensitivity = nil }
	topology := cpu.Topology
	if topology == nil { topology = &vmopv1.VirtualMachineClassCPUTopologySpec{} }
	if vm.Spec.CPUAdvanced.Topology == nil { vm.Spec.CPUAdvanced.Topology = &vmopv1.VirtualMachineCPUTopologySpec{} }
	if value := vmopv1.ClassIntDefault(topology.CoresPerSocket); value != nil || reset { vm.Spec.CPUAdvanced.Topology.CoresPerSocket = value }
	if value := vmopv1.ClassIntDefault(topology.VNUMANodeCount); value != nil || reset { vm.Spec.CPUAdvanced.Topology.VNUMANodeCount = value }
	if vm.Spec.CPUAdvanced.Topology.CoresPerSocket == nil && vm.Spec.CPUAdvanced.Topology.VNUMANodeCount == nil { vm.Spec.CPUAdvanced.Topology = nil }
	if *vm.Spec.CPUAdvanced == (vmopv1.VirtualMachineCPUAdvancedSpec{}) { vm.Spec.CPUAdvanced = nil }
	if *vm.Spec.MemoryAdvanced == (vmopv1.VirtualMachineMemoryAdvancedSpec{}) { vm.Spec.MemoryAdvanced = nil }
}
