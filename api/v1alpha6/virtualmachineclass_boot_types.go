// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

// VirtualMachineClassBootOptions describes class-supplied boot defaults and
// policies. Delays use milliseconds; VirtualMachine boot delays remain durations.
// Boot order is selected by the VM and is not a class setting.
type VirtualMachineClassBootOptions struct {
	// EFISecureBoot enables signature checking by EFI firmware.
	// +optional
	EFISecureBoot *BoolPolicy `json:"efiSecureBoot,omitempty"`

	// BootDelay delays the boot sequence, in milliseconds.
	// Bounds fit the VirtualMachine duration representation.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self.min) || (self.min >= 0 && self.min <= 9223372036854)",message="boot delay min must be a nonnegative duration in milliseconds"
	// +kubebuilder:validation:XValidation:rule="!has(self.max) || (self.max >= 0 && self.max <= 9223372036854)",message="boot delay max must be a nonnegative duration in milliseconds"
	// +kubebuilder:validation:XValidation:rule="!has(self.default) || (self.default >= 0 && self.default <= 9223372036854)",message="boot delay default must be a nonnegative duration in milliseconds"
	BootDelay *LongRange `json:"bootDelay,omitempty"`

	// BootRetry retries a failed boot after BootRetryDelay.
	// +optional
	BootRetry *BoolPolicy `json:"bootRetry,omitempty"`

	// BootRetryDelay delays another boot attempt, in milliseconds.
	// Bounds fit the VirtualMachine duration representation.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!has(self.min) || (self.min >= 0 && self.min <= 9223372036854)",message="boot retry delay min must be a nonnegative duration in milliseconds"
	// +kubebuilder:validation:XValidation:rule="!has(self.max) || (self.max >= 0 && self.max <= 9223372036854)",message="boot retry delay max must be a nonnegative duration in milliseconds"
	// +kubebuilder:validation:XValidation:rule="!has(self.default) || (self.default >= 0 && self.default <= 9223372036854)",message="boot retry delay default must be a nonnegative duration in milliseconds"
	BootRetryDelay *LongRange `json:"bootRetryDelay,omitempty"`
}
