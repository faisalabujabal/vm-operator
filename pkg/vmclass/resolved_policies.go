// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"cmp"
	"reflect"
	"slices"
	"strings"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
)

// resolvePolicyFields publishes marginal domains. Cross-field dependencies,
// image selection, and placement remain separate checks.
func resolvePolicyFields(class, ceiling *vmopv1.VirtualMachineClassSpec, clusters []resolvedCluster) []vmopv1.VirtualMachineClassResolvedField {
	var fields []vmopv1.VirtualMachineClassResolvedField
	for _, path := range []string{
		"cpuAdvanced.hotAddEnabled", "cpuAdvanced.hotRemoveEnabled", "cpuAdvanced.iommuEnabled",
		"cpuAdvanced.nestedHardwareVirtualizationEnabled", "cpuAdvanced.performanceCountersEnabled",
		"memoryAdvanced.hotAddEnabled", "memoryAdvanced.reservationLockedToMax",
		"bootOptions.bootRetry", "bootOptions.efiSecureBoot", "advanced.changeBlockTracking",
		"advanced.preferHtEnabled", "advanced.hugePages1GEnabled", "advanced.timeTrackerLowLatencyEnabled",
		"advanced.cpuAffinityExclusiveNoStatsEnabled", "advanced.vmxSwapEnabled",
	} {
		policy := &vmopv1.VirtualMachineClassResolvedBooleanDomain{Values: []bool{false, true}}
		for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
			if p, ok := resolvedPolicyAt(spec, path).(*vmopv1.BoolPolicy); ok && p.AllowOverride != nil && !*p.AllowOverride {
				policy.Values = slices.DeleteFunc(policy.Values, func(v bool) bool { return p.Default == nil || v != *p.Default })
			}
		}
		f := unknownPolicyField(path, &vmopv1.VirtualMachineClassResolvedDomain{Boolean: policy})
		available := &vmopv1.VirtualMachineClassResolvedBooleanDomain{}
		complete := len(clusters) > 0
		for _, c := range clusters {
			supported, known := resolvedBooleanSupport(path, c)
			complete = complete && known
			if known {
				for _, value := range policy.Values {
					if (!value || supported) && !slices.Contains(available.Values, value) {
						available.Values = append(available.Values, value)
					}
				}
			}
		}
		if complete {
			slices.SortFunc(available.Values, func(a, b bool) int {
				if a == b {
					return 0
				}
				if !a {
					return -1
				}
				return 1
			})
			f.State, f.Reason = "Complete", "EnvironmentBrowser"
			f.Available = &vmopv1.VirtualMachineClassResolvedDomain{Boolean: available}
		}
		fields = append(fields, f)
	}
	for _, path := range []string{"cpuAdvanced.latencySensitivity", "reservedProfileName"} {
		p := &vmopv1.VirtualMachineClassResolvedStringDomain{Unrestricted: true}
		for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
			if policy, ok := resolvedPolicyAt(spec, path).(*vmopv1.EnumPolicy); ok {
				p.Required = p.Required || (policy.Required != nil && *policy.Required)
				if policy.Allowed != nil {
					if p.Unrestricted {
						p.Values = slices.Clone(*policy.Allowed)
					} else {
						p.Values = slices.DeleteFunc(p.Values, func(v string) bool { return !slices.Contains(*policy.Allowed, v) })
					}
					p.Unrestricted = false
				}
			}
		}
		if p.Required {
			p.Values = slices.DeleteFunc(p.Values, func(v string) bool { return v == "" })
		}
		slices.Sort(p.Values)
		p.Values = slices.Compact(p.Values)
		f := unknownPolicyField(path, &vmopv1.VirtualMachineClassResolvedDomain{String: p})
		if path == "reservedProfileName" {
			f.Reason = "ReservedProfileDiscoveryUnavailable"
		}
		fields = append(fields, f)
	}
	for _, path := range []string{"cpuAdvanced.topology.coresPerSocket", "cpuAdvanced.topology.vnumaNodeCount", "bootOptions.bootDelay", "bootOptions.bootRetryDelay"} {
		fields = append(fields, resolvedIntegerField(path, class, ceiling, clusters))
	}
	fields = append(fields, resolvedNUMAField(class, ceiling, clusters), resolvedExtraConfigField(class, ceiling))
	return fields
}

func unknownPolicyField(path string, policy *vmopv1.VirtualMachineClassResolvedDomain) vmopv1.VirtualMachineClassResolvedField {
	return vmopv1.VirtualMachineClassResolvedField{Path: path, State: "Unknown", Reason: "HardwareEvidenceUnavailable", Policy: policy}
}

// resolvedPolicyAt reads only the explicitly enumerated policy paths above.
func resolvedPolicyAt(spec *vmopv1.VirtualMachineClassSpec, path string) any {
	value := reflect.ValueOf(spec)
	for _, part := range strings.Split(path, ".") {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return nil
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return nil
		}
		found := false
		for i := range value.NumField() {
			if strings.Split(value.Type().Field(i).Tag.Get("json"), ",")[0] == part {
				value, found = value.Field(i), true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil
	}
	return value.Interface()
}

func resolvedBooleanSupport(path string, c resolvedCluster) (bool, bool) {
	guest := c.guest
	switch path {
	case "cpuAdvanced.hotAddEnabled", "cpuAdvanced.hotRemoveEnabled", "cpuAdvanced.iommuEnabled", "memoryAdvanced.hotAddEnabled":
		if guest == nil {
			return false, false
		}
		switch path {
		case "cpuAdvanced.hotAddEnabled":
			return guest.SupportsCpuHotAdd, true
		case "cpuAdvanced.hotRemoveEnabled":
			return guest.SupportsCpuHotRemove, true
		case "memoryAdvanced.hotAddEnabled":
			return guest.SupportsMemoryHotAdd, true
		default:
			if guest.VvtdSupported != nil {
				return guest.VvtdSupported.Supported, true
			}
			return false, false
		}
	}
	if c.options == nil || c.options.Status.Capabilities == nil {
		return false, false
	}
	capability := c.options.Status.Capabilities
	switch path {
	case "cpuAdvanced.nestedHardwareVirtualizationEnabled":
		return capability.NestedHVSupported, true
	case "cpuAdvanced.performanceCountersEnabled":
		return capability.VPMCSupported, true
	case "memoryAdvanced.reservationLockedToMax":
		return capability.MemoryReservationLockSupported, true
	case "bootOptions.bootRetry":
		return capability.BootRetryOptionsSupported, true
	case "advanced.changeBlockTracking":
		return capability.ChangeTrackingSupported, true
	case "bootOptions.efiSecureBoot":
		if capability.SecureBootSupported == nil {
			return false, false
		}
		if c.guestSpecified {
			if guest == nil || guest.SupportsSecureBoot == nil {
				return false, false
			}
			return *capability.SecureBootSupported && *guest.SupportsSecureBoot, true
		}
		return *capability.SecureBootSupported, true
	default:
		return false, false
	}
}

func resolvedIntegerField(path string, class, ceiling *vmopv1.VirtualMachineClassSpec, clusters []resolvedCluster) vmopv1.VirtualMachineClassResolvedField {
	r := resolvedIntegerPolicy(path, class, ceiling)
	policy := &vmopv1.VirtualMachineClassResolvedIntegerDomain{}
	if validResolvedIntegerRange(r) {
		policy.Ranges = append(policy.Ranges, r)
	}
	f := unknownPolicyField(path, &vmopv1.VirtualMachineClassResolvedDomain{Integer: policy})
	if strings.HasPrefix(path, "bootOptions.") {
		return f
	}
	available := &vmopv1.VirtualMachineClassResolvedIntegerDomain{}
	if len(clusters) == 0 {
		return f
	}
	for _, c := range clusters {
		low, high, automatic, known := resolvedTopologyOption(path, c, r)
		if !known {
			return f
		}
		if len(policy.Ranges) == 0 {
			continue
		}
		clipped := intersectResolvedIntegers(r, vmopv1.VirtualMachineClassResolvedIntegerRange{Min: &low, Max: &high})
		if validResolvedIntegerRange(clipped) {
			available.Ranges = append(available.Ranges, clipped)
		}
		// Zero selects automatic topology rather than a zero-node machine.
		if automatic {
			available.Values = []int64{0}
		}
	}
	slices.SortFunc(available.Ranges, func(a, b vmopv1.VirtualMachineClassResolvedIntegerRange) int {
		if n := cmp.Compare(*a.Min, *b.Min); n != 0 {
			return n
		}
		return cmp.Compare(*a.Max, *b.Max)
	})
	available.Ranges = slices.CompactFunc(available.Ranges, func(a, b vmopv1.VirtualMachineClassResolvedIntegerRange) bool {
		return *a.Min == *b.Min && *a.Max == *b.Max
	})
	f.State, f.Reason, f.Available = "Complete", "EnvironmentBrowser", &vmopv1.VirtualMachineClassResolvedDomain{Integer: available}
	return f
}

func resolvedTopologyOption(path string, c resolvedCluster, policy vmopv1.VirtualMachineClassResolvedIntegerRange) (int64, int64, bool, bool) {
	if c.options == nil || c.options.Status.HardwareOptions == nil {
		return 0, 0, false, false
	}
	hw := c.options.Status.HardwareOptions
	cores := path == "cpuAdvanced.topology.coresPerSocket"
	var option *vimv1.IntOption
	if cores {
		option = hw.NumCoresPerSocket
	} else {
		option = hw.NumNumaNodes
	}
	if option == nil || option.Min < 0 || option.Max < option.Min {
		return 0, 0, false, false
	}
	low, high := max(int64(option.Min), 1), int64(option.Max)
	automatic := (policy.Min == nil || *policy.Min <= 0) && (policy.Max == nil || *policy.Max >= 0)
	if automatic && cores {
		if hw.AutoCoresPerSocket == nil {
			return 0, 0, false, false
		}
		automatic = hw.AutoCoresPerSocket.Supported
	}
	if cores && high > 1 && (policy.Max == nil || *policy.Max > 1) {
		if c.options.Status.Capabilities == nil {
			return 0, 0, false, false
		}
		if !c.options.Status.Capabilities.MultipleCoresPerSocketSupported {
			high = 1
		}
	}
	if c.guestSpecified && cores {
		if c.guest == nil || c.guest.NumSupportedCoresPerSocket <= 0 {
			return 0, 0, false, false
		}
		high = min(high, int64(c.guest.NumSupportedCoresPerSocket))
	}
	return low, high, automatic, true
}

func resolvedIntegerPolicy(path string, class, ceiling *vmopv1.VirtualMachineClassSpec) vmopv1.VirtualMachineClassResolvedIntegerRange {
	r := vmopv1.VirtualMachineClassResolvedIntegerRange{}
	for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
		var low, high *int64
		switch p := resolvedPolicyAt(spec, path).(type) {
		case *vmopv1.IntRange:
			if p.Min != nil {
				n := int64(*p.Min)
				low = &n
			}
			if p.Max != nil {
				n := int64(*p.Max)
				high = &n
			}
		case *vmopv1.LongRange:
			low, high = p.Min, p.Max
		}
		r = intersectResolvedIntegers(r, vmopv1.VirtualMachineClassResolvedIntegerRange{Min: low, Max: high})
	}
	return r
}

func intersectResolvedIntegers(a, b vmopv1.VirtualMachineClassResolvedIntegerRange) vmopv1.VirtualMachineClassResolvedIntegerRange {
	if b.Min != nil && (a.Min == nil || *b.Min > *a.Min) {
		n := *b.Min
		a.Min = &n
	}
	if b.Max != nil && (a.Max == nil || *b.Max < *a.Max) {
		n := *b.Max
		a.Max = &n
	}
	return a
}

func validResolvedIntegerRange(r vmopv1.VirtualMachineClassResolvedIntegerRange) bool {
	return r.Min == nil || r.Max == nil || *r.Min <= *r.Max
}

func resolvedNUMAField(class, ceiling *vmopv1.VirtualMachineClassSpec, clusters []resolvedCluster) vmopv1.VirtualMachineClassResolvedField {
	const path = "advanced.pnumaNodeAffinity"
	p := &vmopv1.VirtualMachineClassResolvedSetDomain{Unrestricted: true}
	for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
		policy, ok := resolvedPolicyAt(spec, path).(*vmopv1.Int32SetPolicy)
		if !ok {
			continue
		}
		p.Required = p.Required || (policy.Required != nil && *policy.Required)
		if policy.Allowed == nil {
			continue
		}
		sets := canonicalResolvedSets(*policy.Allowed)
		if p.Unrestricted {
			p.Sets = sets
		} else {
			p.Sets = slices.DeleteFunc(p.Sets, func(s vmopv1.Int32Set) bool {
				return !slices.ContainsFunc(sets, func(t vmopv1.Int32Set) bool { return slices.Equal(s.Members, t.Members) })
			})
		}
		p.Unrestricted = false
	}
	// An optional set always permits empty, independently of approved sets.
	if !p.Unrestricted && !p.Required {
		p.Sets = append(p.Sets, vmopv1.Int32Set{Members: []int32{}})
	}
	if p.Required {
		p.Sets = slices.DeleteFunc(p.Sets, func(s vmopv1.Int32Set) bool { return len(s.Members) == 0 })
	}
	p.Sets = canonicalResolvedSets(p.Sets)
	f := unknownPolicyField(path, &vmopv1.VirtualMachineClassResolvedDomain{Set: p})
	if len(clusters) == 0 {
		return f
	}
	var maximum int32
	for _, c := range clusters {
		if c.target == nil || c.target.Status.NumNumaNodes <= 0 {
			return f
		}
		maximum = max(maximum, c.target.Status.NumNumaNodes)
	}
	// IDs are contiguous, so the largest cluster domain also contains every
	// smaller cluster's whole-set domain; this union does not synthesize sets.
	members := make([]int32, maximum)
	for i := range members {
		members[i] = int32(i)
	}
	available := &vmopv1.VirtualMachineClassResolvedSetDomain{Unrestricted: p.Unrestricted, Required: p.Required, Sets: canonicalResolvedSets(p.Sets)}
	if p.Unrestricted {
		available.Members = &members
	}
	available.Sets = slices.DeleteFunc(available.Sets, func(s vmopv1.Int32Set) bool {
		return slices.ContainsFunc(s.Members, func(id int32) bool { return id < 0 || id >= maximum })
	})
	f.State, f.Reason, f.Available = "Complete", "EnvironmentBrowser", &vmopv1.VirtualMachineClassResolvedDomain{Set: available}
	return f
}

func canonicalResolvedSets(input []vmopv1.Int32Set) []vmopv1.Int32Set {
	sets := make([]vmopv1.Int32Set, 0, len(input))
	for _, s := range input {
		members := slices.Clone(s.Members)
		slices.Sort(members)
		members = slices.Compact(members)
		if members == nil {
			members = []int32{}
		}
		sets = append(sets, vmopv1.Int32Set{Members: members})
	}
	slices.SortFunc(sets, func(a, b vmopv1.Int32Set) int { return slices.Compare(a.Members, b.Members) })
	return slices.CompactFunc(sets, func(a, b vmopv1.Int32Set) bool { return slices.Equal(a.Members, b.Members) })
}

func resolvedExtraConfigField(class, ceiling *vmopv1.VirtualMachineClassSpec) vmopv1.VirtualMachineClassResolvedField {
	const path = "advanced.extraConfig"
	p := &vmopv1.VirtualMachineClassResolvedExtraConfigDomain{}
	for _, spec := range []*vmopv1.VirtualMachineClassSpec{class, ceiling} {
		policy, ok := resolvedPolicyAt(spec, path).(*vmopv1.ExtraConfigListPolicy)
		if !ok {
			continue
		}
		isolated := policy.DeepCopy()
		if isolated.Allowed != nil {
			p.Allowed = append(p.Allowed, vmopv1.VirtualMachineClassExtraConfigMatcherGroup{Matchers: resolvedExtraConfigMatchers(*isolated.Allowed)})
		}
		if isolated.Denied != nil {
			p.Denied = append(p.Denied, resolvedExtraConfigMatchers(*isolated.Denied)...)
		}
	}
	f := unknownPolicyField(path, &vmopv1.VirtualMachineClassResolvedDomain{ExtraConfig: p})
	f.Reason = "NonFiniteDomain"
	return f
}
