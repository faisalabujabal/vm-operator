// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"

	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
)

func classAdvancedBoolFields(a *VirtualMachineClassAdvancedSpec) []struct {
	key    string
	policy **BoolPolicy
} {
	return []struct {
		key    string
		policy **BoolPolicy
	}{
		{"numa.vcpu.preferHT", &a.PreferHTEnabled},
		{"sched.mem.lpage.enable1GPage", &a.HugePages1GEnabled},
		{"timeTracker.lowLatency", &a.TimeTrackerLowLatencyEnabled},
		{"sched.cpu.affinity.exclusiveNoStats", &a.CPUAffinityExclusiveNoStatsEnabled},
		{"sched.swap.vmxSwapEnabled", &a.VMXSwapEnabled},
	}
}

func promoteClassAdvanced(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	spec.Advanced = nil
	a := &VirtualMachineClassAdvancedSpec{}
	var cbt bool
	if ok, err := doc.Get("changeTrackingEnabled", &cbt); err == nil && ok {
		a.ChangeBlockTracking = FixedClassBool(cbt)
		if err := doc.Delete("changeTrackingEnabled"); err != nil {
			return err
		}
	}
	var entries []json.RawMessage
	// Unsupported legacy shapes remain in the residual document.
	if _, err := doc.Get("extraConfig", &entries); err != nil {
		entries = nil
	}
	counts := map[string]int{}
	for _, raw := range entries {
		var item struct {
			Key *string `json:"key"`
		}
		if json.Unmarshal(raw, &item) == nil && item.Key != nil {
			counts[*item.Key]++
		}
	}
	var residual []json.RawMessage
	var defaults []vmopv1common.KeyValuePair
	for _, raw := range entries {

		item, err := classExtraConfigStringItem(raw)
		if err != nil || item == nil || counts[item.Key] != 1 {
			residual = append(residual, raw)
			continue
		}
		key, value := item.Key, item.Value
		supported := true

		promoted := false
		for _, f := range classAdvancedBoolFields(a) {
			if key != f.key {
				continue
			}
			v, err := strconv.ParseBool(value)
			if err == nil {
				*f.policy = FixedClassBool(v)
				promoted = true
			}
			supported = false
			break
		}
		if key == "numa.nodeAffinity" {

			if members := classNUMAMembers(value); members != nil {
				sets := []Int32Set{{Members: append([]int32{}, (*members)...)}}
				a.PNUMANodeAffinity = &Int32SetPolicy{Default: members, Allowed: &sets}
				promoted = true
			}

			supported = false
		}
		if promoted {
			continue
		}
		if !supported {
			residual = append(residual, raw)
			continue
		}
		defaults = append(defaults, vmopv1common.KeyValuePair{Key: key, Value: value})
	}
	if len(defaults) > 0 {
		a.ExtraConfig = &ExtraConfigListPolicy{Default: &defaults}
	}
	if len(residual) > 0 {
		if err := doc.Set("extraConfig", residual); err != nil {
			return err
		}
	} else if len(entries) > 0 {
		if err := doc.Delete("extraConfig"); err != nil {
			return err
		}
	}
	if a.ChangeBlockTracking != nil || a.PNUMANodeAffinity != nil || a.ExtraConfig != nil {
		spec.Advanced = a
	}
	for _, f := range classAdvancedBoolFields(a) {
		if *f.policy != nil {
			spec.Advanced = a
			break
		}
	}
	return nil
}

func projectClassAdvanced(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	a := spec.Advanced
	if a == nil {
		return nil
	}
	if a.ChangeBlockTracking != nil && a.ChangeBlockTracking.Default != nil {
		if err := doc.Set("changeTrackingEnabled", *a.ChangeBlockTracking.Default); err != nil {
			return err
		}
	}
	values := []vmopv1common.KeyValuePair{}
	if a.ExtraConfig != nil && a.ExtraConfig.Default != nil {
		values = append(values, (*a.ExtraConfig.Default)...)
	}
	for _, f := range classAdvancedBoolFields(a) {
		if *f.policy != nil && (*f.policy).Default != nil {
			values = append(values, vmopv1common.KeyValuePair{Key: f.key, Value: strings.ToUpper(strconv.FormatBool(*(*f.policy).Default))})
		}
	}
	if a.PNUMANodeAffinity != nil && a.PNUMANodeAffinity.Default != nil {
		parts := []string{}
		for _, n := range *a.PNUMANodeAffinity.Default {
			parts = append(parts, strconv.FormatInt(int64(n), 10))
		}
		values = append(values, vmopv1common.KeyValuePair{Key: "numa.nodeAffinity", Value: strings.Join(parts, ",")})
	}

	if len(values) == 0 {
		return nil
	}
	var entries []json.RawMessage
	if _, err := doc.Get("extraConfig", &entries); err != nil {
		// An older writer may have stored an unsupported bag shape. Keep it
		// intact instead of fabricating a valid replacement; the conversion
		// annotation retains policies that cannot be projected into this bag.
		return nil
	}
	entries, err := overlayClassExtraConfig(entries, values)
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		return doc.Set("extraConfig", entries)
	}
	return nil
}

func restoreClassAdvanced(dst, preserved, projected *VirtualMachineClassSpec) {
	if preserved.Advanced == nil {
		return
	}
	wasAbsent := dst.Advanced == nil
	if dst.Advanced == nil {
		dst.Advanced = &VirtualMachineClassAdvancedSpec{}
	}
	p := projected.Advanced
	if p == nil {
		p = &VirtualMachineClassAdvancedSpec{}
	}
	d, a := dst.Advanced, preserved.Advanced
	restoreUnchangedClassPolicy(&d.ChangeBlockTracking, a.ChangeBlockTracking, p.ChangeBlockTracking)
	ds, as, ps := classAdvancedBoolFields(d), classAdvancedBoolFields(a), classAdvancedBoolFields(p)
	for i := range ds {
		restoreUnchangedClassPolicy(ds[i].policy, *as[i].policy, *ps[i].policy)
	}
	restoreUnchangedClassPolicy(&d.PNUMANodeAffinity, a.PNUMANodeAffinity, p.PNUMANodeAffinity)
	restoreUnchangedClassPolicy(&d.ExtraConfig, a.ExtraConfig, p.ExtraConfig)
	if wasAbsent && apiequality.Semantic.DeepEqual(d, &VirtualMachineClassAdvancedSpec{}) && !apiequality.Semantic.DeepEqual(a, &VirtualMachineClassAdvancedSpec{}) {
		dst.Advanced = nil
	}
}

func decodeClassExtraConfigString(raw json.RawMessage, value *string) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	if json.Unmarshal(raw, value) == nil {
		return true
	}
	var typed map[string]json.RawMessage
	if json.Unmarshal(raw, &typed) != nil || len(typed) != 2 {
		return false
	}
	var name string
	return json.Unmarshal(typed["_typeName"], &name) == nil && name == "string" && !bytes.Equal(bytes.TrimSpace(typed["_value"]), []byte("null")) && json.Unmarshal(typed["_value"], value) == nil
}

// overlayClassExtraConfig decodes each raw entry once. Targeted duplicates
// contribute unknown sibling fields to one replacement; untouched duplicates
// and unsupported entries retain their original order and JSON. Typed keys use
// their last value and appear in last-occurrence order, matching repeated overlay.
func overlayClassExtraConfig(entries []json.RawMessage, values []vmopv1common.KeyValuePair) ([]json.RawMessage, error) {
	last := make(map[string]int, len(values))
	for i, value := range values {
		last[value.Key] = i
	}
	items := make(map[string]map[string]json.RawMessage, len(last))
	result := make([]json.RawMessage, 0, len(entries)+len(last))
	for _, raw := range entries {
		var candidate map[string]json.RawMessage
		var key string
		if json.Unmarshal(raw, &candidate) != nil || bytes.Equal(bytes.TrimSpace(candidate["key"]), []byte("null")) || json.Unmarshal(candidate["key"], &key) != nil {
			result = append(result, raw)
			continue
		}
		if _, targeted := last[key]; !targeted {
			result = append(result, raw)
			continue
		}
		item := items[key]
		if item == nil {
			item = map[string]json.RawMessage{}
			items[key] = item
		}
		for name, field := range candidate {
			item[name] = field
		}
	}
	for i, value := range values {
		if last[value.Key] != i {
			continue
		}
		item := items[value.Key]
		if item == nil {
			item = map[string]json.RawMessage{}
		}
		if _, ok := item["_typeName"]; !ok {
			item["_typeName"] = json.RawMessage(`"OptionValue"`)
		}
		key, err := json.Marshal(value.Key)
		if err != nil {
			return nil, err
		}
		item["key"] = key
		encodedValue, err := json.Marshal(map[string]string{"_typeName": "string", "_value": value.Value})
		if err != nil {
			return nil, err
		}
		item["value"] = encodedValue
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, nil
}

func classExtraConfigStringItem(raw json.RawMessage) (*vmopv1common.KeyValuePair, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode class ExtraConfig item: %w", err)
	}
	return classExtraConfigStringFields(item), nil
}

// classExtraConfigStringFields returns nil for options with unsupported value
// types or unknown metadata, so their original JSON remains in ConfigSpec.
func classExtraConfigStringFields(item map[string]json.RawMessage) *vmopv1common.KeyValuePair {
	var result vmopv1common.KeyValuePair
	if bytes.Equal(bytes.TrimSpace(item["key"]), []byte("null")) || json.Unmarshal(item["key"], &result.Key) != nil || !decodeClassExtraConfigString(item["value"], &result.Value) {
		return nil
	}
	if rawType, ok := item["_typeName"]; ok {
		var name string
		if json.Unmarshal(rawType, &name) != nil || name != "OptionValue" {
			return nil
		}
	}
	for name := range item {
		if name != "key" && name != "value" && name != "_typeName" {
			return nil
		}
	}
	return &result
}

func classNUMAMembers(value string) *[]int32 {
	members := []int32{}
	if value == "" {
		return &members
	}
	seen := map[int32]bool{}
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 32)
		if err != nil || n < 0 || seen[int32(n)] {
			return nil
		}
		members = append(members, int32(n))
		seen[int32(n)] = true
	}
	return &members
}
