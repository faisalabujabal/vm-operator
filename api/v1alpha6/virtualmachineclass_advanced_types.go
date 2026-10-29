// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"

// Int32Set contains one approved set of integer members.
type Int32Set struct {
	// Members describes the set, with ordering ignored.
	// +required
	// +kubebuilder:validation:XValidation:rule="self.all(n, n >= 0)",message="NUMA node IDs must be nonnegative"
	// +listType=set
	Members []int32 `json:"members"`
}

// Int32SetPolicy describes a default set and approved alternative sets.
// +kubebuilder:validation:XValidation:rule="!has(self.default) || self.default.all(n, n >= 0)",message="NUMA node IDs must be nonnegative"
// +kubebuilder:validation:XValidation:rule="!has(self.required) || !self.required || !has(self.default) || size(self.default) > 0",message="a required set cannot have an empty default"
// +kubebuilder:validation:XValidation:rule="!has(self.default) || !has(self.allowed) || self.allowed.exists(s, size(s.members) == size(self.default) && s.members.all(n, n in self.default))",message="default must be an allowed set"
type Int32SetPolicy struct {
	// Default is the set materialized on the VM.
	// +optional
	// +listType=set
	Default *[]int32 `json:"default,omitempty"`
	// Allowed contains approved sets. Omission permits any valid set.
	// +optional
	// +listType=atomic
	Allowed *[]Int32Set `json:"allowed,omitempty"`
	// Required rejects an empty set when true.
	// +optional
	Required *bool `json:"required,omitempty"`
}

// StringMatcher matches a string using exactly one matching mode.
// +kubebuilder:validation:XValidation:rule="(has(self.fixed) ? 1 : 0) + (has(self.glob) ? 1 : 0) + (has(self.regex) ? 1 : 0) == 1",message="exactly one of fixed, glob or regex must be set"
type StringMatcher struct {
	// Fixed matches a literal string.
	// +optional
	Fixed *string `json:"fixed,omitempty"`
	// Glob matches a shell-style glob pattern.
	// +optional
	Glob *string `json:"glob,omitempty"`
	// Regex matches a regular expression.
	// +optional
	Regex *string `json:"regex,omitempty"`
}

// ExtraConfigMatcher matches an ExtraConfig item's key. Omission matches any key.
type ExtraConfigMatcher struct {
	// Key matches the item's key.
	// +optional
	Key *StringMatcher `json:"key,omitempty"`
}

// ExtraConfigListPolicy describes default ExtraConfig values and key matchers.
// Denied matchers take precedence over allowed matchers.
type ExtraConfigListPolicy struct {
	// Default is materialized on the VM at admission.
	// +optional
	// +listType=map
	// +listMapKey=key
	Default *[]vmopv1common.KeyValuePair `json:"default,omitempty"`
	// Allowed contains permitted key matchers. Omission leaves the list open.
	// +optional
	// +listType=atomic
	Allowed *[]ExtraConfigMatcher `json:"allowed,omitempty"`
	// Denied contains prohibited key matchers.
	// +optional
	// +listType=atomic
	Denied *[]ExtraConfigMatcher `json:"denied,omitempty"`
}

// VirtualMachineClassAdvancedSpec describes advanced VM configuration policies.
type VirtualMachineClassAdvancedSpec struct {
	// ChangeBlockTracking enables incremental backup support.
	// +optional
	ChangeBlockTracking *BoolPolicy `json:"changeBlockTracking,omitempty"`
	// PreferHTEnabled prefers hyperthreads of the same physical core.
	// +optional
	PreferHTEnabled *BoolPolicy `json:"preferHtEnabled,omitempty"`
	// HugePages1GEnabled enables 1 GB huge-page memory backing.
	// +optional
	HugePages1GEnabled *BoolPolicy `json:"hugePages1GEnabled,omitempty"`
	// TimeTrackerLowLatencyEnabled enables low-latency time tracking.
	// +optional
	TimeTrackerLowLatencyEnabled *BoolPolicy `json:"timeTrackerLowLatencyEnabled,omitempty"`
	// CPUAffinityExclusiveNoStatsEnabled disables per-VM CPU accounting.
	// +optional
	CPUAffinityExclusiveNoStatsEnabled *BoolPolicy `json:"cpuAffinityExclusiveNoStatsEnabled,omitempty"`
	// VMXSwapEnabled permits swapping the VMX process's memory.
	// +optional
	VMXSwapEnabled *BoolPolicy `json:"vmxSwapEnabled,omitempty"`
	// PNUMANodeAffinity pins the VM to physical NUMA node IDs.
	// +optional
	PNUMANodeAffinity *Int32SetPolicy `json:"pnumaNodeAffinity,omitempty"`
	// ExtraConfig describes VMX properties without a typed policy.
	// +optional
	ExtraConfig *ExtraConfigListPolicy `json:"extraConfig,omitempty"`
}
