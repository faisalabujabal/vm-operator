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

// StringMatcher matches a string key using exactly one matching operation.
// +kubebuilder:validation:XValidation:rule="has(self.fixed) ? (!has(self.glob) && !has(self.regex)) : (has(self.glob) != has(self.regex))",message="exactly one of fixed, glob or regex must be set"
type StringMatcher struct {
	// Fixed matches an exact key.
	// +optional
	Fixed *string `json:"fixed,omitempty"`
	// Glob matches a key using a glob expression.
	// +optional
	Glob *string `json:"glob,omitempty"`
	// Regex matches a key using a regular expression.
	// +optional
	Regex *string `json:"regex,omitempty"`
}

// VGPUDeviceMatcher matches the identifying fields of a vGPU device.
type VGPUDeviceMatcher struct {
	// ProfileName matches the profile name; omission matches every profile.
	// +optional
	ProfileName *StringMatcher `json:"profileName,omitempty"`
}

// DynamicDirectPathIODeviceMatcher matches a Dynamic DirectPath I/O identity.
type DynamicDirectPathIODeviceMatcher struct {
	// CustomLabel matches the device backing label; omission matches any label.
	// +optional
	CustomLabel *StringMatcher `json:"customLabel,omitempty"`

	// VendorID matches the vendor; omission matches every vendor.
	// +optional
	VendorID *int64 `json:"vendorID,omitempty"`
	// DeviceID matches the device; omission matches every device.
	// +optional
	DeviceID *int64 `json:"deviceID,omitempty"`
}

// VGPUDeviceListPolicy describes default vGPU devices and key constraints.
type VGPUDeviceListPolicy struct {
	// Default contains devices supplied by the class.
	// +optional
	// +listType=map
	// +listMapKey=profileName
	Default *[]VGPUDevice `json:"default,omitempty"`
	// Allowed matches permitted device identities.
	// +optional
	// +listType=atomic
	Allowed *[]VGPUDeviceMatcher `json:"allowed,omitempty"`
	// Denied matches forbidden identities and takes precedence over Allowed.
	// +optional
	// +listType=atomic
	Denied *[]VGPUDeviceMatcher `json:"denied,omitempty"`
}

// DynamicDirectPathIODeviceListPolicy describes default devices and constraints.
type DynamicDirectPathIODeviceListPolicy struct {
	// Default contains devices supplied by the class.
	// +optional
	// +listType=map
	// +listMapKey=vendorID
	// +listMapKey=deviceID
	// +listMapKey=customLabel
	Default *[]DynamicDirectPathIODevice `json:"default,omitempty"`
	// Allowed matches permitted device identities.
	// +optional
	// +listType=atomic
	Allowed *[]DynamicDirectPathIODeviceMatcher `json:"allowed,omitempty"`
	// Denied matches forbidden identities and takes precedence over Allowed.
	// +optional
	// +listType=atomic
	Denied *[]DynamicDirectPathIODeviceMatcher `json:"denied,omitempty"`
}

// GetDefault returns the configured vGPU defaults, preserving omission.
func (p *VGPUDeviceListPolicy) GetDefault() *[]VGPUDevice {
	if p == nil {
		return nil
	}
	return p.Default
}

// GetDefault returns the configured DirectPath defaults, preserving omission.
func (p *DynamicDirectPathIODeviceListPolicy) GetDefault() *[]DynamicDirectPathIODevice {
	if p == nil {
		return nil
	}
	return p.Default
}
