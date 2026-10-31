// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

// ZoneSelection contains zones compatible with the values already supplied by
// the VM. Unresolved fields are not treated as violations: they require an
// explicit zone only when the surviving governors imply different defaults or
// constraints. This does not materialize defaults or replace full governance
// validation of an effective VM configuration.
type ZoneSelection struct {
	Zones           []string
	Governors       map[string]string
	Excluded        map[string]string
	AmbiguousFields []string
}

// ResolveZones reads current namespace classes and evaluates candidate zones.
// Callers supply namespace zone names already narrowed by placement constraints.
// Admission uses an uncached reader; placement evaluates again before DRS.
func ResolveZones(ctx context.Context, reader ctrlclient.Reader, vm *vmopv1.VirtualMachine, zones []string) (ZoneSelection, error) {
	return ResolveZonesWithFrequency(ctx, reader, vm, zones, nil)
}

// ResolveZonesWithFrequency additionally accepts per-zone cluster frequency for placement.
// Multi-cluster zones must be checked independently before combining eligible pools.
func ResolveZonesWithFrequency(ctx context.Context, reader ctrlclient.Reader, vm *vmopv1.VirtualMachine, zones []string, frequencies map[string]int64) (ZoneSelection, error) {
	var classes vmopv1.VirtualMachineClassList
	if err := reader.List(ctx, &classes, ctrlclient.InNamespace(vm.Namespace)); err != nil {
		return ZoneSelection{}, fmt.Errorf("read zone governance: %w", err)
	}
	var class *vmopv1.VirtualMachineClass
	for i := range classes.Items {
		if classes.Items[i].Name == vm.Spec.ClassName {
			class = &classes.Items[i]
			break
		}
	}
	hardware := class
	// Live class hardware conditions are not evidence about an older snapshot.
	if class != nil && vm.Spec.Class != nil && vm.Spec.Class.Name != "" {
		var instance vmopv1.VirtualMachineClassInstance
		if err := reader.Get(ctx, ctrlclient.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.Class.Name}, &instance); err != nil {
			return ZoneSelection{}, fmt.Errorf("read class snapshot for zone selection: %w", err)
		}
		liveHash, err := vmclass.ConfigurationHash(ctx, &class.Spec)
		if err != nil {
			return ZoneSelection{}, err
		}
		snapshotHash, err := vmclass.ConfigurationHash(ctx, &instance.Spec.VirtualMachineClassSpec)
		if err != nil {
			return ZoneSelection{}, err
		}
		if liveHash != snapshotHash {
			hardware = nil
		}
	}
	return selectZones(vm, zones, classes.Items, class, hardware, frequencies)
}

func selectZones(vm *vmopv1.VirtualMachine, zones []string, classes []vmopv1.VirtualMachineClass, class, hardware *vmopv1.VirtualMachineClass, frequencies map[string]int64) (ZoneSelection, error) {
	result := ZoneSelection{Governors: map[string]string{}, Excluded: map[string]string{}}
	encoded, err := json.Marshal(&vm.Spec)
	if err != nil {
		return result, err
	}
	values := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return result, err
	}
	unresolved := map[string]map[string]string{}
	zones = slices.Clone(zones)
	slices.Sort(zones)
	zones = slices.Compact(zones)
	for _, zone := range zones {
		if class != nil && !Available(class, zone) {
			result.Excluded[zone] = "class is unavailable"
			continue
		}
		if reason := hardwareRejection(hardware, zone); reason != "" {
			result.Excluded[zone] = reason
			continue
		}
		governor, err := ResolveClasses(classes, vm.Namespace, zone)
		if err != nil {
			return result, err
		}
		missing := map[string]string{}
		if governor != nil {
			result.Governors[zone] = governor.Name
			var violations []string
			for _, p := range policies(&governor.Spec) {
				value := lookup(values, p.path)
				if p.path == "resources.requests.memory" && vm.Spec.MemoryAdvanced != nil && vm.Spec.MemoryAdvanced.ReservationLockedToMax != nil && *vm.Spec.MemoryAdvanced.ReservationLockedToMax {
					value = lookup(values, "resources.size.memory")
				}
				switch {
				case value == nil:
					signature, err := unresolvedPolicy(p.value)
					if err != nil {
						return result, err
					}
					if signature != "" {
						missing[p.path] = signature
					}
				case (p.path == "resources.requests.cpu" || p.path == "resources.limits.cpu") && frequencies[zone] <= 0:
					// A Supervisor-wide frequency cannot prove a zone's vCPU ceiling.
					// Preserve the uncertainty until per-cluster frequency is resolved.
					minimum, maximum, _ := bounds(p.value)
					if minimum != nil || maximum != nil {
						raw, err := json.Marshal(p.value)
						if err != nil {
							return result, err
						}
						missing[p.path] = "frequency:" + string(raw)
					}
				default:
					if err := checkValue(value, p.value, p.path, frequencies[zone]); err != nil {
						violations = append(violations, p.path+": "+err.Error())
					}
				}
			}
			if len(violations) > 0 {
				result.Excluded[zone] = strings.Join(violations, "; ")
				continue
			}
		}
		result.Zones = append(result.Zones, zone)
		unresolved[zone] = missing
	}
	result.findAmbiguousFields(unresolved)
	return result, nil
}

// Different governor identities alone do not create ambiguity. Compare only
// unresolved values; fully specified VMs may fit several different governors.
func (s *ZoneSelection) findAmbiguousFields(unresolved map[string]map[string]string) {
	if len(s.Zones) < 2 {
		return
	}
	paths := map[string]bool{}
	for _, missing := range unresolved {
		for path := range missing {
			paths[path] = true
		}
	}
	for path := range paths {
		first := unresolved[s.Zones[0]][path]
		if strings.HasPrefix(first, "frequency:") {
			s.AmbiguousFields = append(s.AmbiguousFields, path)
			continue
		}
		for _, zone := range s.Zones[1:] {
			if unresolved[zone][path] != first {
				s.AmbiguousFields = append(s.AmbiguousFields, path)
				break
			}
		}
	}
	slices.Sort(s.AmbiguousFields)
}

func unresolvedPolicy(policy any) (string, error) {
	var def any
	if p, ok := policy.(*vmopv1.EnumPolicy); ok {
		if value := vmopv1.ClassEnumDefault(p); value != nil {
			def = value
		} else if !isRequired(p.Required) {
			return "", nil
		}
	} else {
		v := reflect.ValueOf(policy).Elem().FieldByName("Default")
		if v.IsValid() && !v.IsNil() {
			if v.Elem().Kind() == reflect.Slice && v.Elem().Len() == 0 {
				return "", nil
			}
			def = v.Interface()
		}
	}
	if def != nil {
		return defaultSignature(def)
	}
	// An absent optional list is already empty unless governance supplies a
	// default. Allow/deny matchers do not add devices or ExtraConfig entries.
	switch p := policy.(type) {
	case *vmopv1.VGPUDeviceListPolicy, *vmopv1.DynamicDirectPathIODeviceListPolicy, *vmopv1.ExtraConfigListPolicy:
		return "", nil
	case *vmopv1.BoolPolicy:
		if !isFixed(p) {
			return "", nil
		}
	case *vmopv1.Int32SetPolicy:
		if !isRequired(p.Required) {
			return "", nil
		}
	}
	raw, err := json.Marshal(policy)
	if string(raw) == "{}" {
		return "", err
	}
	return "unresolved:" + string(raw), err
}

func hardwareRejection(class *vmopv1.VirtualMachineClass, zone string) string {
	if class == nil || class.Status.ObservedGeneration != class.Generation {
		return ""
	}
	for _, entry := range class.Status.Zones {
		if entry.Name != zone {
			continue
		}
		condition := apimeta.FindStatusCondition(entry.Conditions, vmopv1.VirtualMachineClassConditionHardwareCompatible)
		if condition != nil && condition.ObservedGeneration == class.Generation && condition.Status == metav1.ConditionFalse {
			return "class hardware is incompatible: " + condition.Reason + ": " + condition.Message
		}
	}
	return ""
}

// Error reports an empty eligible set or unresolved zone-dependent defaults.
// The diagnostics contain namespace-visible class names and policy fields only.
func (s ZoneSelection) Error() error {
	if len(s.Zones) == 0 {
		reasons := make([]string, 0, len(s.Excluded))
		for zone, reason := range s.Excluded {
			reasons = append(reasons, zone+" ("+reason+")")
		}
		slices.Sort(reasons)
		return fmt.Errorf("no eligible zones: %s", strings.Join(reasons, ", "))
	}
	if len(s.AmbiguousFields) > 0 {
		var choices []string
		for _, zone := range s.Zones {
			governor := s.Governors[zone]
			if governor == "" {
				governor = "none"
			}
			choices = append(choices, zone+" (governor "+governor+")")
		}
		return fmt.Errorf("specify a zone or resolve zone-dependent fields %s; candidates: %s", strings.Join(s.AmbiguousFields, ", "), strings.Join(choices, ", "))
	}
	return nil
}

// Default lists are API sets or maps, so their item order is immaterial.
func defaultSignature(value any) (string, error) {
	if q, ok := value.(*resource.Quantity); ok {
		mantissa, exponent := q.AsCanonicalBytes(nil)
		return fmt.Sprintf("default:quantity:%sE%d", mantissa, exponent), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(raw) > 0 && raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", err
		}
		slices.SortFunc(items, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
		raw, err = json.Marshal(items)
	}
	return "default:" + string(raw), err
}
