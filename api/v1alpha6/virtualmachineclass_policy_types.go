// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import "k8s.io/apimachinery/pkg/api/resource"

// ResourceQuantityRange describes a default with optional inclusive bounds.
// An omitted bound is open. Bounds do not supply an implicit default.
// +kubebuilder:validation:XValidation:rule="(!has(self.min) && !has(self.max)) || has(self.default)",message="a bounded range requires a default"
// +kubebuilder:validation:XValidation:rule="!has(self.min) || !has(self.default) || quantity(string(self.default)).compareTo(quantity(string(self.min))) >= 0",message="default must not be below min"
// +kubebuilder:validation:XValidation:rule="!has(self.max) || !has(self.default) || quantity(string(self.default)).compareTo(quantity(string(self.max))) <= 0",message="default must not exceed max"
type ResourceQuantityRange struct {
	// Min is the inclusive lower bound.
	// +optional
	Min *resource.Quantity `json:"min,omitempty"`
	// Max is the inclusive upper bound.
	// +optional
	Max *resource.Quantity `json:"max,omitempty"`
	// Default is the value supplied by this class.
	// +optional
	Default *resource.Quantity `json:"default,omitempty"`
}

// BoolPolicy describes a boolean default and whether it can be overridden.
// +kubebuilder:validation:XValidation:rule="!has(self.allowOverride) || self.allowOverride || has(self.default)",message="a fixed boolean requires a default"
type BoolPolicy struct {
	// Default is the value supplied by the class.
	// +optional
	Default *bool `json:"default,omitempty"`
	// AllowOverride permits a different value below this class.
	// +optional
	AllowOverride *bool `json:"allowOverride,omitempty"`
}

// IntRange describes a 32-bit integer default and optional inclusive bounds.
// +kubebuilder:validation:XValidation:rule="(!has(self.min) && !has(self.max)) || has(self.default)",message="a bounded range requires a default"
// +kubebuilder:validation:XValidation:rule="!has(self.min) || !has(self.default) || self.default >= self.min",message="default must not be below min"
// +kubebuilder:validation:XValidation:rule="!has(self.max) || !has(self.default) || self.default <= self.max",message="default must not exceed max"
type IntRange struct {
	// Min is the inclusive lower bound.
	// +optional
	Min *int32 `json:"min,omitempty"`
	// Max is the inclusive upper bound.
	// +optional
	Max *int32 `json:"max,omitempty"`
	// Default is the value supplied by the class.
	// +optional
	Default *int32 `json:"default,omitempty"`
}

// LongRange describes a 64-bit integer default and optional inclusive bounds.
// +kubebuilder:validation:XValidation:rule="(!has(self.min) && !has(self.max)) || has(self.default)",message="a bounded range requires a default"
// +kubebuilder:validation:XValidation:rule="!has(self.min) || !has(self.default) || self.default >= self.min",message="default must not be below min"
// +kubebuilder:validation:XValidation:rule="!has(self.max) || !has(self.default) || self.default <= self.max",message="default must not exceed max"
type LongRange struct {
	// Min is the inclusive lower bound.
	// +optional
	Min *int64 `json:"min,omitempty"`
	// Max is the inclusive upper bound.
	// +optional
	Max *int64 `json:"max,omitempty"`
	// Default is the value supplied by the class.
	// +optional
	Default *int64 `json:"default,omitempty"`
}

// EnumPolicy describes a scalar default and permitted nonempty values.
// +kubebuilder:validation:XValidation:rule="!has(self.allowed) || size(self.allowed) <= 1 || has(self.default)",message="multiple allowed values require a default"
// +kubebuilder:validation:XValidation:rule="!has(self.default) || self.default == '' || !has(self.allowed) || self.default in self.allowed",message="default must be allowed"
type EnumPolicy struct {
	// Default is the value supplied by the class.
	// +optional
	Default *string `json:"default,omitempty"`
	// Allowed contains permitted nonempty values; omission differs from [].
	// +optional
	// +listType=set
	Allowed *[]string `json:"allowed,omitempty"`
	// Required disallows an empty result after defaulting.
	// +optional
	Required *bool `json:"required,omitempty"`
}
