// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import "k8s.io/apimachinery/pkg/api/resource"

// VirtualMachineClassResolvedField describes one field's marginal domain in a
// zone. Choices from different fields do not promise a jointly placeable VM.
type VirtualMachineClassResolvedField struct {
	// Path is the field path relative to the class spec.
	// +required
	Path string `json:"path"`
	// State is Complete when Available is authoritative for the discovered
	// domain, or Unknown when hardware evidence is incomplete.
	// +required
	// +kubebuilder:validation:Enum=Complete;Unknown
	State string `json:"state"`
	// Reason explains the source or absence of resolved choices.
	// +required
	Reason string `json:"reason"`
	// Policy contains the effective class and governance restrictions, without
	// defaults. These restrictions alone do not establish hardware support.
	// +optional
	Policy *VirtualMachineClassResolvedDomain `json:"policy,omitempty"`
	// Available contains hardware choices within Policy. It is absent when
	// State is Unknown. An empty typed domain means no choices are available.
	// +optional
	Available *VirtualMachineClassResolvedDomain `json:"available,omitempty"`
}

// VirtualMachineClassResolvedDomain holds one typed field domain. Exactly one
// member describes a field. Empty typed domains differ from absent evidence.
type VirtualMachineClassResolvedDomain struct {
	// Quantity describes CPU quantities or memory bytes.
	// +optional
	Quantity *VirtualMachineClassResolvedQuantityDomain `json:"quantity,omitempty"`
	// Integer describes integral values in the field's documented units.
	// +optional
	Integer *VirtualMachineClassResolvedIntegerDomain `json:"integer,omitempty"`
	// Boolean contains permitted boolean values.
	// +optional
	Boolean *VirtualMachineClassResolvedBooleanDomain `json:"boolean,omitempty"`
	// String contains permitted scalar strings.
	// +optional
	String *VirtualMachineClassResolvedStringDomain `json:"string,omitempty"`
	// Set describes whole integer sets or the domain of their members.
	// +optional
	Set *VirtualMachineClassResolvedSetDomain `json:"set,omitempty"`
	// ExtraConfig describes effective VMX key constraints.
	// +optional
	ExtraConfig *VirtualMachineClassResolvedExtraConfigDomain `json:"extraConfig,omitempty"`
	// VGPU describes vGPU profile identities and their display information.
	// +optional
	VGPU *VirtualMachineClassResolvedVGPUDomain `json:"vgpu,omitempty"`
	// DirectPath describes Dynamic DirectPath identities and display information.
	// +optional
	DirectPath *VirtualMachineClassResolvedDirectPathDomain `json:"directPath,omitempty"`
}

// VirtualMachineClassResolvedQuantityDomain is the union of ranges and values.
// A range with no endpoints is unrestricted; empty ranges and values mean none.
type VirtualMachineClassResolvedQuantityDomain struct {
	// Ranges contains inclusive intervals, without filling gaps between them.
	// +optional
	// +listType=atomic
	Ranges []VirtualMachineClassResolvedQuantityRange `json:"ranges,omitempty"`
	// Values contains exact permitted values, including noncontiguous CPU counts.
	// +optional
	// +listType=atomic
	Values []resource.Quantity `json:"values,omitempty"`
}

// VirtualMachineClassResolvedQuantityRange contains inclusive quantity bounds.
type VirtualMachineClassResolvedQuantityRange struct {
	// Min is the lower bound, or absent when unbounded.
	// +optional
	Min *resource.Quantity `json:"min,omitempty"`
	// Max is the upper bound, or absent when unbounded.
	// +optional
	Max *resource.Quantity `json:"max,omitempty"`
}

// VirtualMachineClassResolvedIntegerDomain is the union of ranges and values.
type VirtualMachineClassResolvedIntegerDomain struct {
	// Ranges contains inclusive intervals.
	// +optional
	// +listType=atomic
	Ranges []VirtualMachineClassResolvedIntegerRange `json:"ranges,omitempty"`
	// Values contains exact permitted values.
	// +optional
	// +listType=set
	Values []int64 `json:"values,omitempty"`
}

// VirtualMachineClassResolvedIntegerRange contains inclusive integer bounds.
type VirtualMachineClassResolvedIntegerRange struct {
	// Min is the lower bound, or absent when unbounded.
	// +optional
	Min *int64 `json:"min,omitempty"`
	// Max is the upper bound, or absent when unbounded.
	// +optional
	Max *int64 `json:"max,omitempty"`
}

// VirtualMachineClassResolvedBooleanDomain enumerates boolean choices.
type VirtualMachineClassResolvedBooleanDomain struct {
	// Values contains the permitted values; empty means none.
	// +optional
	// +listType=set
	Values []bool `json:"values,omitempty"`
}

// VirtualMachineClassResolvedStringDomain enumerates scalar string choices.
type VirtualMachineClassResolvedStringDomain struct {
	// Unrestricted means Values does not restrict the string domain.
	// +optional
	Unrestricted bool `json:"unrestricted,omitempty"`
	// Values contains permitted strings; empty means none unless unrestricted.
	// +optional
	// +listType=set
	Values []string `json:"values,omitempty"`
	// Required rejects an empty value.
	// +optional
	Required bool `json:"required,omitempty"`
}

// VirtualMachineClassResolvedSetDomain preserves approved whole sets. Members
// may constrain the universe without enumerating its power set.
type VirtualMachineClassResolvedSetDomain struct {
	// Unrestricted means Sets does not restrict the choice of whole sets.
	// +optional
	Unrestricted bool `json:"unrestricted,omitempty"`
	// Sets contains approved whole sets, never an intersection of their members.
	// +optional
	// +listType=atomic
	Sets []Int32Set `json:"sets,omitempty"`
	// Members contains valid member IDs when known; nil means unknown.
	// +optional
	// +listType=set
	Members *[]int32 `json:"members,omitempty"`
	// Required rejects the empty set.
	// +optional
	Required bool `json:"required,omitempty"`
}

// VirtualMachineClassResolvedExtraConfigDomain retains matcher conjunctions.
type VirtualMachineClassResolvedExtraConfigDomain struct {
	// Allowed requires a key to match at least one matcher in every group.
	// No groups means unrestricted; an empty group allows no keys.
	// +optional
	// +listType=atomic
	Allowed []VirtualMachineClassExtraConfigMatcherGroup `json:"allowed,omitempty"`
	// Denied rejects matching keys even when allowed.
	// +optional
	// +listType=atomic
	Denied []VirtualMachineClassResolvedExtraConfigMatcher `json:"denied,omitempty"`
}

// VirtualMachineClassExtraConfigMatcherGroup is one disjunction of matchers.
type VirtualMachineClassExtraConfigMatcherGroup struct {
	// Matchers contains alternatives within this group.
	// +optional
	// +listType=atomic
	Matchers []VirtualMachineClassResolvedExtraConfigMatcher `json:"matchers,omitempty"`
}

// VirtualMachineClassResolvedVGPUDomain describes vGPU choices and restrictions.
type VirtualMachineClassResolvedVGPUDomain struct {
	// Allowed requires matching at least one matcher in every group.
	// +optional
	// +listType=atomic
	Allowed []VirtualMachineClassVGPUMatcherGroup `json:"allowed,omitempty"`
	// Denied rejects matching identities.
	// +optional
	// +listType=atomic
	Denied []VirtualMachineClassResolvedVGPUMatcher `json:"denied,omitempty"`
	// Values contains discovered and permitted profiles.
	// +optional
	// +listType=atomic
	Values []VirtualMachineClassResolvedVGPU `json:"values,omitempty"`
}

// VirtualMachineClassVGPUMatcherGroup is one disjunction of vGPU matchers.
type VirtualMachineClassVGPUMatcherGroup struct {
	// Matchers contains alternatives within this group.
	// +optional
	// +listType=atomic
	Matchers []VirtualMachineClassResolvedVGPUMatcher `json:"matchers,omitempty"`
}

// VirtualMachineClassResolvedVGPU exposes only profile identity and display data.
type VirtualMachineClassResolvedVGPU struct {
	// ProfileName identifies the profile.
	// +required
	ProfileName string `json:"profileName"`
	// DeviceVendorID identifies the supporting PCI model, not a host or device instance.
	// +optional
	DeviceVendorID int64 `json:"deviceVendorID,omitempty"`
	// FBSizeInGiB is the framebuffer size per VM, in gibibytes.
	// +optional
	FBSizeInGiB int64 `json:"fbSizeInGiB,omitempty"`
	// ProfileSharing describes profile sharing behavior.
	// +optional
	ProfileSharing string `json:"profileSharing,omitempty"`
	// ProfileClass describes the profile class.
	// +optional
	ProfileClass string `json:"profileClass,omitempty"`
}

// VirtualMachineClassResolvedDirectPathDomain describes permitted PCI devices.
type VirtualMachineClassResolvedDirectPathDomain struct {
	// Allowed requires matching at least one matcher in every group.
	// +optional
	// +listType=atomic
	Allowed []VirtualMachineClassDirectPathMatcherGroup `json:"allowed,omitempty"`
	// Denied rejects matching identities.
	// +optional
	// +listType=atomic
	Denied []VirtualMachineClassResolvedDirectPathMatcher `json:"denied,omitempty"`
	// Values contains discovered and permitted identities.
	// +optional
	// +listType=atomic
	Values []VirtualMachineClassResolvedDirectPath `json:"values,omitempty"`
}

// VirtualMachineClassDirectPathMatcherGroup is one disjunction of PCI matchers.
type VirtualMachineClassDirectPathMatcherGroup struct {
	// Matchers contains alternatives within this group.
	// +optional
	// +listType=atomic
	Matchers []VirtualMachineClassResolvedDirectPathMatcher `json:"matchers,omitempty"`
}

// VirtualMachineClassResolvedDirectPath exposes PCI identity and display data.
type VirtualMachineClassResolvedDirectPath struct {
	// VendorID identifies the PCI vendor.
	// +required
	VendorID int64 `json:"vendorID"`
	// DeviceID identifies the PCI device model.
	// +required
	DeviceID int64 `json:"deviceID"`
	// CustomLabel identifies the device backing label.
	// +optional
	CustomLabel string `json:"customLabel,omitempty"`
	// VendorName is the display name of the vendor.
	// +optional
	VendorName string `json:"vendorName,omitempty"`
	// DeviceName is the display name of the device model.
	// +optional
	DeviceName string `json:"deviceName,omitempty"`
}

// VirtualMachineClassResolvedStringMatcher reports a validated string matcher.
// Status uses its own shape so spec admission CEL is not repeated per zone.
type VirtualMachineClassResolvedStringMatcher struct {
	// Fixed matches a literal value.
	// +optional
	Fixed *string `json:"fixed,omitempty"`
	// Glob matches a shell-style glob.
	// +optional
	Glob *string `json:"glob,omitempty"`
	// Regex matches a regular expression.
	// +optional
	Regex *string `json:"regex,omitempty"`
}

// VirtualMachineClassResolvedExtraConfigMatcher describes a VMX key constraint.
type VirtualMachineClassResolvedExtraConfigMatcher struct {
	// Key matches a VMX key; absence matches every key.
	// +optional
	Key *VirtualMachineClassResolvedStringMatcher `json:"key,omitempty"`
}

// VirtualMachineClassResolvedVGPUMatcher describes a profile identity constraint.
type VirtualMachineClassResolvedVGPUMatcher struct {
	// ProfileName matches a profile name; absence matches every profile.
	// +optional
	ProfileName *VirtualMachineClassResolvedStringMatcher `json:"profileName,omitempty"`
}

// VirtualMachineClassResolvedDirectPathMatcher describes a PCI identity constraint.
type VirtualMachineClassResolvedDirectPathMatcher struct {
	// VendorID matches the PCI vendor; absence matches every vendor.
	// +optional
	VendorID *int64 `json:"vendorID,omitempty"`
	// DeviceID matches the PCI device model; absence matches every model.
	// +optional
	DeviceID *int64 `json:"deviceID,omitempty"`
	// CustomLabel matches a backing label; absence matches every label.
	// +optional
	CustomLabel *VirtualMachineClassResolvedStringMatcher `json:"customLabel,omitempty"`
}
