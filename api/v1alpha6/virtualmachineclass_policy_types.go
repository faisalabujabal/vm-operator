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

// EnumPolicy describes a default and the permitted scalar values.
// +kubebuilder:validation:XValidation:rule="!has(self.allowed) || size(self.allowed) <= 1 || has(self.default)",message="multiple allowed values require a default"
// +kubebuilder:validation:XValidation:rule="!has(self.default) || !has(self.allowed) || self.default in self.allowed",message="default must be allowed"
type EnumPolicy struct {
	// Default is the value supplied by this class.
	// +optional
	Default *string `json:"default,omitempty"`
	// Allowed contains the permitted values; omission differs from an empty list.
	// +optional
	// +listType=set
	Allowed *[]string `json:"allowed,omitempty"`
	// Required disallows an empty value after defaulting.
	// +optional
	Required *bool `json:"required,omitempty"`
}
