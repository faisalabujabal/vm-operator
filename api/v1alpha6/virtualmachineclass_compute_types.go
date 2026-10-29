// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

// VirtualMachineClassCPUTopologySpec describes permitted guest CPU topology.
// +kubebuilder:validation:XValidation:rule="!has(self.vnumaNodeCount) || !has(self.vnumaNodeCount.default) || self.vnumaNodeCount.default == 0 || (has(self.coresPerSocket) && has(self.coresPerSocket.default) && self.coresPerSocket.default > 0)",message="a positive vnumaNodeCount default requires positive coresPerSocket default"
type VirtualMachineClassCPUTopologySpec struct {
	// CoresPerSocket is the number of cores per virtual socket; zero means auto.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self.min) || self.min >= 0",message="min must be nonnegative"
	// +kubebuilder:validation:XValidation:rule="!has(self.max) || self.max >= 0",message="max must be nonnegative"
	// +kubebuilder:validation:XValidation:rule="!has(self.default) || self.default >= 0",message="default must be nonnegative"
	CoresPerSocket *IntRange `json:"coresPerSocket,omitempty"`
	// VNUMANodeCount is the number of virtual NUMA nodes; zero means auto.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self.min) || self.min >= 0",message="min must be nonnegative"
	// +kubebuilder:validation:XValidation:rule="!has(self.max) || self.max >= 0",message="max must be nonnegative"
	// +kubebuilder:validation:XValidation:rule="!has(self.default) || self.default >= 0",message="default must be nonnegative"
	VNUMANodeCount *IntRange `json:"vnumaNodeCount,omitempty"`
}

// VirtualMachineClassCPUAdvancedSpec describes class cpu presets and policy.
type VirtualMachineClassCPUAdvancedSpec struct {
	// Topology describes guest CPU topology.
	// +optional
	Topology *VirtualMachineClassCPUTopologySpec `json:"topology,omitempty"`
	// LatencySensitivity selects Normal, High or HighWithHyperthreading.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self.default) || self.default in ['Normal', 'High', 'HighWithHyperthreading']",message="unsupported latency sensitivity default"
	LatencySensitivity *EnumPolicy `json:"latencySensitivity,omitempty"`
	// HotAddEnabled controls the cpuHotAddEnabled setting.
	// +optional
	HotAddEnabled *BoolPolicy `json:"hotAddEnabled,omitempty"`
	// HotRemoveEnabled controls the cpuHotRemoveEnabled setting.
	// +optional
	HotRemoveEnabled *BoolPolicy `json:"hotRemoveEnabled,omitempty"`
	// IOMMUEnabled controls the flags.vvtdEnabled setting.
	// +optional
	IOMMUEnabled *BoolPolicy `json:"iommuEnabled,omitempty"`
	// NestedHardwareVirtualizationEnabled controls the nestedHVEnabled setting.
	// +optional
	NestedHardwareVirtualizationEnabled *BoolPolicy `json:"nestedHardwareVirtualizationEnabled,omitempty"`
	// PerformanceCountersEnabled controls the vPMCEnabled setting.
	// +optional
	PerformanceCountersEnabled *BoolPolicy `json:"performanceCountersEnabled,omitempty"`
	// ExposeVNUMAOnCPUHotAdd controls the virtualNuma.exposeVnumaOnCpuHotadd setting.
	// +optional
	ExposeVNUMAOnCPUHotAdd *BoolPolicy `json:"exposeVnumaOnCpuHotadd,omitempty"`
}

// VirtualMachineClassMemoryAdvancedSpec describes class memory presets and policy.
type VirtualMachineClassMemoryAdvancedSpec struct {
	// HotAddEnabled controls the memoryHotAddEnabled setting.
	// +optional
	HotAddEnabled *BoolPolicy `json:"hotAddEnabled,omitempty"`
	// ReservationLockedToMax controls the memoryReservationLockedToMax setting.
	// +optional
	ReservationLockedToMax *BoolPolicy `json:"reservationLockedToMax,omitempty"`
}
