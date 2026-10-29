// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

// VirtualMachineClassCPUTopologySpec describes permitted guest CPU topology.
type VirtualMachineClassCPUTopologySpec struct {
	// CoresPerSocket is the number of cores per virtual socket; zero means auto.
	// +optional
	CoresPerSocket *IntRange `json:"coresPerSocket,omitempty"`
	// VNUMANodeCount is the number of virtual NUMA nodes; zero means auto.
	// +optional
	VNUMANodeCount *IntRange `json:"vnumaNodeCount,omitempty"`
}

// VirtualMachineClassCPUAdvancedSpec describes class cpu presets and policy.
type VirtualMachineClassCPUAdvancedSpec struct {
	// Topology describes guest CPU topology.
	// +optional
	Topology *VirtualMachineClassCPUTopologySpec `json:"topology,omitempty"`
	// LatencySensitivity selects Normal, High or HighWithHyperthreading.
	// +optional
	LatencySensitivity *EnumPolicy `json:"latencySensitivity,omitempty"`
	// HotAddEnabled governs the cpuHotAddEnabled setting.
	// +optional
	HotAddEnabled *BoolPolicy `json:"hotAddEnabled,omitempty"`
	// HotRemoveEnabled governs the cpuHotRemoveEnabled setting.
	// +optional
	HotRemoveEnabled *BoolPolicy `json:"hotRemoveEnabled,omitempty"`
	// IOMMUEnabled governs the flags.vvtdEnabled setting.
	// +optional
	IOMMUEnabled *BoolPolicy `json:"iommuEnabled,omitempty"`
	// NestedHardwareVirtualizationEnabled governs the nestedHVEnabled setting.
	// +optional
	NestedHardwareVirtualizationEnabled *BoolPolicy `json:"nestedHardwareVirtualizationEnabled,omitempty"`
	// PerformanceCountersEnabled governs the vPMCEnabled setting.
	// +optional
	PerformanceCountersEnabled *BoolPolicy `json:"performanceCountersEnabled,omitempty"`
	// ExposeVNUMAOnCPUHotAdd governs the virtualNuma.exposeVnumaOnCpuHotadd setting.
	// +optional
	ExposeVNUMAOnCPUHotAdd *BoolPolicy `json:"exposeVnumaOnCpuHotadd,omitempty"`
}

// VirtualMachineClassMemoryAdvancedSpec describes class memory presets and policy.
type VirtualMachineClassMemoryAdvancedSpec struct {
	// HotAddEnabled governs the memoryHotAddEnabled setting.
	// +optional
	HotAddEnabled *BoolPolicy `json:"hotAddEnabled,omitempty"`
	// ReservationLockedToMax governs the memoryReservationLockedToMax setting.
	// +optional
	ReservationLockedToMax *BoolPolicy `json:"reservationLockedToMax,omitempty"`
}

