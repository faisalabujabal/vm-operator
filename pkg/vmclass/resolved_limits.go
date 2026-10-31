// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	"github.com/vmware-tanzu/vm-operator/pkg/util"
)

// ResolveClassLimits computes per-field domains from one discovery and policy
// snapshot. Runtime callers gate this operation on VMServiceClasslessVM. The
// result describes marginal choices, not joint placement or free capacity.
// Missing evidence in any cluster suppresses available choices for that field;
// policy-only bounds remain explicitly separate. Inputs are never mutated.
func ResolveClassLimits(class *vmopv1.VirtualMachineClass, classes []vmopv1.VirtualMachineClass,
	zone string, clusterIDs []string, targets []vimv1.ConfigTarget,
	options []vimv1.VirtualMachineConfigOptions, guests []vimv1.VirtualMachineGuestOptions) []vmopv1.VirtualMachineClassResolvedField {
	ceiling, err := governance.ResolveClasses(classes, class.Namespace, zone)
	var ceilingSpec *vmopv1.VirtualMachineClassSpec
	if ceiling != nil {
		ceilingSpec = &ceiling.Spec
	}
	clusters := resolvedClusters(&class.Spec, clusterIDs, targets, options, guests)
	fields := resolveResourceFields(&class.Spec, ceilingSpec, clusters)
	if err != nil {
		for i := range fields {
			fields[i].State = "Unknown"
			fields[i].Reason = "GovernanceUnresolved"
			fields[i].Policy = nil
			fields[i].Available = nil
		}
	}
	return fields
}

// resolvedCluster holds only current, uniquely identified discovery evidence.
// It never becomes part of public status, which must not expose inventory IDs.
type resolvedCluster struct {
	target         *vimv1.ConfigTarget
	options        *vimv1.VirtualMachineConfigOptions
	guest          *vimv1.VirtualMachineGuestOptionsHardwareVersionStatus
	guestSpecified bool
}

func resolvedClusters(spec *vmopv1.VirtualMachineClassSpec, ids []string, targets []vimv1.ConfigTarget,
	options []vimv1.VirtualMachineConfigOptions, guests []vimv1.VirtualMachineGuestOptions) []resolvedCluster {
	var version, guestID string
	if len(spec.ConfigSpec) != 0 {
		raw, err := util.UnmarshalConfigSpecFromJSON(spec.ConfigSpec)
		if err != nil {
			return []resolvedCluster{{}}
		}
		version, guestID = raw.Version, raw.GuestId
	}
	if len(ids) == 0 {
		return []resolvedCluster{{guestSpecified: guestID != ""}}
	}
	result := make([]resolvedCluster, 0, len(ids))
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		c := resolvedCluster{guestSpecified: guestID != ""}
		matches := 0
		for i := range targets {
			if targets[i].Spec.ID.ID == id {
				matches++
				c.target = &targets[i]
			}
		}
		if matches != 1 || !discoveryReady(c.target, c.target.Status.ObservedGeneration, c.target.Status.Conditions) {
			c.target = nil
			result = append(result, c)
			continue
		}
		v := version
		if v == "" {
			v = c.target.Status.MaxHardwareVersion
		}
		maximum, maxErr := strconv.Atoi(strings.TrimPrefix(c.target.Status.MaxHardwareVersion, "vmx-"))
		requested, versionErr := strconv.Atoi(strings.TrimPrefix(v, "vmx-"))
		if maxErr != nil || versionErr != nil || maximum <= 0 || requested <= 0 || requested > maximum {
			result = append(result, c)
			continue
		}
		matches = 0
		for i := range options {
			o := &options[i]
			if v != "" && o.Spec.HardwareVersion == v && ownedBy(o, c.target) &&
				o.Annotations["vmoperator.vmware.com/environment-browser-cluster"] == id {
				matches++
				c.options = o
			}
		}
		if matches != 1 || !discoveryReady(c.options, c.options.Status.ObservedGeneration, c.options.Status.Conditions) {
			c.options = nil
			result = append(result, c)
			continue
		}
		c.guest = resolvedGuest(guestID, v, c.options, guests)
		result = append(result, c)
	}
	return result
}

func resolvedGuest(id, version string, options *vimv1.VirtualMachineConfigOptions,
	guests []vimv1.VirtualMachineGuestOptions) *vimv1.VirtualMachineGuestOptionsHardwareVersionStatus {
	if id == "" || !slices.ContainsFunc(options.Status.GuestOSIdentifiers, func(g vimv1.VirtualMachineGuestOSIdentifier) bool {
		return string(g) == id || g.ToVimType() == id
	}) {
		return nil
	}
	var result *vimv1.VirtualMachineGuestOptionsHardwareVersionStatus
	matches := 0
	for i := range guests {
		g := &guests[i]
		if !g.DeletionTimestamp.IsZero() || (string(g.Spec.ID) != id && g.Spec.ID.ToVimType() != id) {
			continue
		}
		for j := range g.Status.HardwareVersions {
			if g.Status.HardwareVersions[j].HardwareVersion == version {
				matches++
				result = &g.Status.HardwareVersions[j]
			}
		}
	}
	if matches != 1 {
		return nil
	}
	return result
}

func resolveResourceFields(class, ceiling *vmopv1.VirtualMachineClassSpec, clusters []resolvedCluster) []vmopv1.VirtualMachineClassResolvedField {
	if ceiling == nil {
		ceiling = &vmopv1.VirtualMachineClassSpec{}
	}
	result := make([]vmopv1.VirtualMachineClassResolvedField, 0, 6)
	for _, field := range []struct {
		path           string
		own, governing *vmopv1.ResourceQuantityRange
	}{
		{"resources.size.cpu", class.Resources.Size.CPU, ceiling.Resources.Size.CPU},
		{"resources.size.memory", class.Resources.Size.Memory, ceiling.Resources.Size.Memory},
		{"resources.requests.cpu", class.Resources.Requests.CPU, ceiling.Resources.Requests.CPU},
		{"resources.requests.memory", class.Resources.Requests.Memory, ceiling.Resources.Requests.Memory},
		{"resources.limits.cpu", class.Resources.Limits.CPU, ceiling.Resources.Limits.CPU},
		{"resources.limits.memory", class.Resources.Limits.Memory, ceiling.Resources.Limits.Memory},
	} {
		policy := resourcePolicy(field.own, field.governing)
		f := vmopv1.VirtualMachineClassResolvedField{Path: field.path, State: "Unknown", Reason: "HardwareEvidenceUnavailable",
			Policy: &vmopv1.VirtualMachineClassResolvedDomain{Quantity: policy}}
		if len(policy.Ranges) == 0 {
			f.State, f.Reason = "Complete", "NoPermittedValues"
			f.Available = &vmopv1.VirtualMachineClassResolvedDomain{Quantity: &vmopv1.VirtualMachineClassResolvedQuantityDomain{}}
		} else if field.path == "resources.size.cpu" || field.path == "resources.size.memory" {
			available := &vmopv1.VirtualMachineClassResolvedQuantityDomain{}
			complete := len(clusters) > 0
			for _, cluster := range clusters {
				var domain *vmopv1.VirtualMachineClassResolvedQuantityDomain
				if field.path == "resources.size.cpu" {
					domain = resolvedCPUs(cluster, policy.Ranges[0])
				} else {
					domain = resolvedMemory(cluster, policy.Ranges[0])
				}
				if domain == nil {
					complete = false
					break
				}
				available.Values = append(available.Values, domain.Values...)
				available.Ranges = append(available.Ranges, domain.Ranges...)
			}
			if complete {
				canonicalQuantityDomain(available)
				f.State, f.Reason = "Complete", "HardwareAndPolicy"
				f.Available = &vmopv1.VirtualMachineClassResolvedDomain{Quantity: available}
				if len(available.Values) == 0 && len(available.Ranges) == 0 {
					f.Reason = "NoPermittedValues"
				}
			}
		}
		result = append(result, f)
	}
	return result
}

func resourcePolicy(policies ...*vmopv1.ResourceQuantityRange) *vmopv1.VirtualMachineClassResolvedQuantityDomain {
	r := vmopv1.VirtualMachineClassResolvedQuantityRange{}
	for _, p := range policies {
		if p != nil {
			r = intersectQuantityRange(r, vmopv1.VirtualMachineClassResolvedQuantityRange{Min: p.Min, Max: p.Max})
		}
	}
	result := &vmopv1.VirtualMachineClassResolvedQuantityDomain{}
	if validQuantityRange(r) {
		result.Ranges = []vmopv1.VirtualMachineClassResolvedQuantityRange{r}
	}
	return result
}

func intersectQuantityRange(a, b vmopv1.VirtualMachineClassResolvedQuantityRange) vmopv1.VirtualMachineClassResolvedQuantityRange {
	if b.Min != nil && (a.Min == nil || b.Min.Cmp(*a.Min) > 0) {
		v := b.Min.DeepCopy()
		a.Min = &v
	}
	if b.Max != nil && (a.Max == nil || b.Max.Cmp(*a.Max) < 0) {
		v := b.Max.DeepCopy()
		a.Max = &v
	}
	return a
}

func validQuantityRange(r vmopv1.VirtualMachineClassResolvedQuantityRange) bool {
	return r.Min == nil || r.Max == nil || r.Min.Cmp(*r.Max) <= 0
}

func resolvedCPUs(c resolvedCluster, policy vmopv1.VirtualMachineClassResolvedQuantityRange) *vmopv1.VirtualMachineClassResolvedQuantityDomain {
	if c.target == nil || c.options == nil || c.options.Status.HardwareOptions == nil ||
		c.target.Status.MaxCPUsPerVM <= 0 || len(c.options.Status.HardwareOptions.NumCPU) == 0 ||
		(c.guestSpecified && (c.guest == nil || c.guest.SupportedMaxCPUs <= 0)) {
		return nil
	}
	maximum := c.target.Status.MaxCPUsPerVM
	if c.guestSpecified && c.guest.SupportedMaxCPUs < maximum {
		maximum = c.guest.SupportedMaxCPUs
	}
	result := &vmopv1.VirtualMachineClassResolvedQuantityDomain{}
	for _, n := range c.options.Status.HardwareOptions.NumCPU {
		if n <= 0 { // Malformed discovery must not become an authoritative domain.
			return nil
		}
		q := *resource.NewQuantity(int64(n), resource.DecimalSI)
		if n <= maximum && (policy.Min == nil || q.Cmp(*policy.Min) >= 0) &&
			(policy.Max == nil || q.Cmp(*policy.Max) <= 0) {
			result.Values = append(result.Values, q)
		}
	}
	return result
}

func resolvedMemory(c resolvedCluster, policy vmopv1.VirtualMachineClassResolvedQuantityRange) *vmopv1.VirtualMachineClassResolvedQuantityDomain {
	if c.target == nil || c.options == nil || c.options.Status.HardwareOptions == nil ||
		c.options.Status.HardwareOptions.Memory == nil || c.target.Status.SupportedMaxMem == nil ||
		c.target.Status.SupportedMaxMem.Sign() <= 0 ||
		(c.guestSpecified && (c.guest == nil || c.guest.SupportedMinMem == nil || c.guest.SupportedMaxMem == nil)) {
		return nil
	}
	memory := c.options.Status.HardwareOptions.Memory
	if memory.Min.Sign() < 0 || memory.Max.Sign() <= 0 || memory.Min.Cmp(memory.Max) > 0 {
		return nil
	}
	r := intersectQuantityRange(policy, vmopv1.VirtualMachineClassResolvedQuantityRange{Min: &memory.Min, Max: &memory.Max})
	r = intersectQuantityRange(r, vmopv1.VirtualMachineClassResolvedQuantityRange{Max: c.target.Status.SupportedMaxMem})
	if c.guestSpecified {
		if c.guest.SupportedMinMem.Sign() < 0 || c.guest.SupportedMaxMem.Sign() <= 0 || c.guest.SupportedMinMem.Cmp(*c.guest.SupportedMaxMem) > 0 {
			return nil
		}
		r = intersectQuantityRange(r, vmopv1.VirtualMachineClassResolvedQuantityRange{Min: c.guest.SupportedMinMem, Max: c.guest.SupportedMaxMem})
	}
	result := &vmopv1.VirtualMachineClassResolvedQuantityDomain{}
	if validQuantityRange(r) {
		result.Ranges = []vmopv1.VirtualMachineClassResolvedQuantityRange{r}
	}
	return result
}

func canonicalQuantityDomain(domain *vmopv1.VirtualMachineClassResolvedQuantityDomain) {
	slices.SortFunc(domain.Values, func(a, b resource.Quantity) int { return a.Cmp(b) })
	domain.Values = slices.CompactFunc(domain.Values, func(a, b resource.Quantity) bool { return a.Cmp(b) == 0 })
	slices.SortFunc(domain.Ranges, func(a, b vmopv1.VirtualMachineClassResolvedQuantityRange) int { return a.Min.Cmp(*b.Min) })
	var merged []vmopv1.VirtualMachineClassResolvedQuantityRange
	for _, r := range domain.Ranges {
		if len(merged) == 0 || merged[len(merged)-1].Max.Cmp(*r.Min) < 0 {
			merged = append(merged, r)
		} else if r.Max.Cmp(*merged[len(merged)-1].Max) > 0 {
			merged[len(merged)-1].Max = r.Max
		}
	}
	domain.Ranges = merged
}
