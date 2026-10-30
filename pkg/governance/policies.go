// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
)

type policyField struct {
	path  string
	value any
}

// policies walks concrete API policy types, rather than assuming every default
// is governable. ConfigSpec, instance storage, and governance metadata are not
// policy types and are therefore never treated as configuration constraints.
func policies(spec *vmopv1.VirtualMachineClassSpec) []policyField {
	var result []policyField
	var walk func(reflect.Value, string)
	walk = func(v reflect.Value, path string) {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			switch v.Interface().(type) {
			case *vmopv1.ResourceQuantityRange, *vmopv1.IntRange, *vmopv1.LongRange,
				*vmopv1.BoolPolicy, *vmopv1.EnumPolicy, *vmopv1.Int32SetPolicy,
				*vmopv1.ExtraConfigListPolicy, *vmopv1.VGPUDeviceListPolicy, *vmopv1.DynamicDirectPathIODeviceListPolicy:
				result = append(result, policyField{path, v.Interface()})
				return
			}
			walk(v.Elem(), path)
			return
		}
		if v.Kind() != reflect.Struct {
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			child := name
			if path != "" {
				child = path + "." + name
			}
			walk(v.Field(i), child)
		}
	}
	if spec != nil {
		walk(reflect.ValueOf(spec), "")
	}
	return result
}

// CheckClass compares each declared class policy with the governing ceiling.
// Omitted fields inherit the ceiling at VM evaluation. Numeric bounds and
// finite boolean/enum/set domains must fit; list defaults are checked item by
// item (T302). Matcher languages are not compared for containment: every
// effective VM item must independently pass the governing list policy.
func CheckClass(class, ceiling *vmopv1.VirtualMachineClassSpec) field.ErrorList {
	candidates := map[string]any{}
	for _, p := range policies(class) {
		candidates[p.path] = p.value
	}
	var errs field.ErrorList
	for _, p := range policies(ceiling) {
		candidate, ok := candidates[p.path]
		if !ok {
			continue
		}
		if err := comparePolicy(candidate, p.value); err != nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", strings.Split(p.path, ".")...), nil, err.Error()))
		}
	}
	return errs
}

// CheckVM evaluates an effective VM spec. The caller resolves class/image and
// observed defaults first; missing constrained scalars fail closed. CPU
// requests/limits are MHz and are compared exactly with class vCPUs using the
// target cluster's minimum host frequency. Size CPU remains a vCPU count.
func CheckVM(effective *vmopv1.VirtualMachineSpec, ceiling *vmopv1.VirtualMachineClassSpec, minHostMHz int64) field.ErrorList {
	if effective == nil {
		return field.ErrorList{field.Required(field.NewPath("spec"), "effective VM configuration is required")}
	}
	encoded, err := json.Marshal(effective)
	if err != nil {
		return field.ErrorList{field.InternalError(field.NewPath("spec"), err)}
	}
	var values map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return field.ErrorList{field.InternalError(field.NewPath("spec"), err)}
	}
	var errs field.ErrorList
	for _, p := range policies(ceiling) {
		value := lookup(values, p.path)
		if p.path == "resources.requests.memory" && effective.MemoryAdvanced != nil &&
			effective.MemoryAdvanced.ReservationLockedToMax != nil && *effective.MemoryAdvanced.ReservationLockedToMax {
			value = lookup(values, "resources.size.memory")
		}
		if err := checkValue(value, p.value, p.path, minHostMHz); err != nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", strings.Split(p.path, ".")...), value, err.Error()))
		}
	}
	return errs
}

func lookup(values map[string]any, path string) any {
	var value any = values
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[part]
	}
	return value
}

func rational(v any) (*big.Rat, error) {
	text := fmt.Sprint(v)
	switch q := v.(type) {
	case resource.Quantity:
		text = q.AsDec().String()
	case *resource.Quantity:
		if q == nil {
			return nil, nil
		}
		quantity := q.DeepCopy()
		text = quantity.AsDec().String()
	case nil:
		return nil, nil
	}
	q, err := resource.ParseQuantity(text)
	if err != nil {
		return nil, fmt.Errorf("invalid numeric value %q", text)
	}
	n, ok := new(big.Rat).SetString(q.AsDec().String())
	if !ok {
		return nil, fmt.Errorf("invalid numeric value %q", text)
	}
	return n, nil
}

func bounds(policy any) (minimum, maximum, def *big.Rat) {
	switch p := policy.(type) {
	case *vmopv1.ResourceQuantityRange:
		minimum, _ = rational(p.Min)
		maximum, _ = rational(p.Max)
		def, _ = rational(p.Default)
	case *vmopv1.IntRange:
		if p.Min != nil {
			minimum = new(big.Rat).SetInt64(int64(*p.Min))
		}
		if p.Max != nil {
			maximum = new(big.Rat).SetInt64(int64(*p.Max))
		}
		if p.Default != nil {
			def = new(big.Rat).SetInt64(int64(*p.Default))
		}
	case *vmopv1.LongRange:
		if p.Min != nil {
			minimum = new(big.Rat).SetInt64(*p.Min)
		}
		if p.Max != nil {
			maximum = new(big.Rat).SetInt64(*p.Max)
		}
		if p.Default != nil {
			def = new(big.Rat).SetInt64(*p.Default)
		}
	}
	return
}

func inRange(value, minimum, maximum *big.Rat) error {
	if value == nil {
		if minimum == nil && maximum == nil {
			return nil
		}
		return fmt.Errorf("effective value is unresolved")
	}
	if minimum != nil && value.Cmp(minimum) < 0 {
		return fmt.Errorf("value is below governing minimum %s", minimum.RatString())
	}
	if maximum != nil && value.Cmp(maximum) > 0 {
		return fmt.Errorf("value exceeds governing maximum %s", maximum.RatString())
	}
	return nil
}

func isRequired(p *bool) bool           { return p != nil && *p }
func isFixed(p *vmopv1.BoolPolicy) bool { return p.AllowOverride != nil && !*p.AllowOverride }

func comparePolicy(candidate, ceiling any) error {
	switch p := ceiling.(type) {
	case *vmopv1.ResourceQuantityRange, *vmopv1.IntRange, *vmopv1.LongRange:
		return compareNumericPolicy(candidate, p)
	case *vmopv1.BoolPolicy:
		c := candidate.(*vmopv1.BoolPolicy)
		if isFixed(p) && (!isFixed(c) || p.Default == nil || c.Default == nil || *p.Default != *c.Default) {
			return fmt.Errorf("class boolean permits a value forbidden by governance")
		}
	case *vmopv1.EnumPolicy:
		c := candidate.(*vmopv1.EnumPolicy)
		if isRequired(p.Required) && !isRequired(c.Required) {
			return fmt.Errorf("class must require this value")
		}
		if p.Allowed != nil {
			if c.Allowed == nil {
				return fmt.Errorf("unbounded class enum exceeds governing values")
			}
			for _, v := range *c.Allowed {
				if err := checkEnum(v, p); err != nil {
					return err
				}
			}
		}
		if v := vmopv1.ClassEnumDefault(c); v != nil {
			return checkEnum(*v, p)
		}
	case *vmopv1.Int32SetPolicy:
		c := candidate.(*vmopv1.Int32SetPolicy)
		if isRequired(p.Required) && !isRequired(c.Required) {
			return fmt.Errorf("class must require a nonempty set")
		}
		if p.Allowed != nil {
			if c.Allowed == nil {
				return fmt.Errorf("unbounded class set exceeds governing sets")
			}
			for _, v := range *c.Allowed {
				if err := checkSet(v.Members, p); err != nil {
					return err
				}
			}
		}
		if c.Default != nil {
			return checkSet(*c.Default, p)
		}
	case *vmopv1.ExtraConfigListPolicy:
		c := candidate.(*vmopv1.ExtraConfigListPolicy)
		return checkItems(c.Default, p.Allowed, p.Denied, matchExtra)
	case *vmopv1.VGPUDeviceListPolicy:
		c := candidate.(*vmopv1.VGPUDeviceListPolicy)
		return checkItems(c.Default, p.Allowed, p.Denied, matchVGPU)
	case *vmopv1.DynamicDirectPathIODeviceListPolicy:
		c := candidate.(*vmopv1.DynamicDirectPathIODeviceListPolicy)
		return checkItems(c.Default, p.Allowed, p.Denied, matchDirect)
	}
	return nil
}

func checkEnum(value string, p *vmopv1.EnumPolicy) error {
	if value == "" && isRequired(p.Required) {
		return fmt.Errorf("a nonempty value is required")
	}
	if p.Allowed != nil && !slices.Contains(*p.Allowed, value) {
		return fmt.Errorf("value is not permitted by governing enum")
	}
	return nil
}

func checkSet(value []int32, p *vmopv1.Int32SetPolicy) error {
	if len(value) == 0 {
		if isRequired(p.Required) {
			return fmt.Errorf("a nonempty set is required")
		}
		return nil
	}
	canonical := slices.Clone(value)
	slices.Sort(canonical)
	canonical = slices.Compact(canonical)
	if p.Allowed == nil {
		return nil
	}
	for _, set := range *p.Allowed {
		members := slices.Clone(set.Members)
		slices.Sort(members)
		members = slices.Compact(members)
		if slices.Equal(canonical, members) {
			return nil
		}
	}
	return fmt.Errorf("set is not permitted by governance")
}

func checkValue(value any, policy any, path string, minHostMHz int64) error {
	switch p := policy.(type) {
	case *vmopv1.ResourceQuantityRange, *vmopv1.IntRange, *vmopv1.LongRange:
		return checkNumericValue(value, p, path, minHostMHz)
	case *vmopv1.BoolPolicy:
		if !isFixed(p) {
			return nil
		}
		if p.Default == nil {
			return fmt.Errorf("governing fixed boolean has no default")
		}
		if path == "bootOptions.bootRetry" || path == "bootOptions.efiSecureBoot" {
			if value == "Enabled" {
				value = true
			} else if value == "Disabled" {
				value = false
			}
		}
		b, ok := value.(bool)
		if !ok {
			return fmt.Errorf("effective boolean is unresolved")
		}
		if b != *p.Default {
			return fmt.Errorf("boolean differs from governing fixed value")
		}
	case *vmopv1.EnumPolicy:
		if value == nil {
			value = ""
		}
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected an enum string")
		}
		return checkEnum(v, p)
	case *vmopv1.Int32SetPolicy:
		v, err := decodeValue[[]int32](value)
		if err != nil {
			return err
		}
		return checkSet(v, p)
	case *vmopv1.ExtraConfigListPolicy:
		v, err := decodeValue[[]vmopv1common.KeyValuePair](value)
		if err != nil {
			return err
		}
		return checkItems(&v, p.Allowed, p.Denied, matchExtra)
	case *vmopv1.VGPUDeviceListPolicy:
		v, err := decodeValue[[]vmopv1.VGPUDevice](value)
		if err != nil {
			return err
		}
		return checkItems(&v, p.Allowed, p.Denied, matchVGPU)
	case *vmopv1.DynamicDirectPathIODeviceListPolicy:
		v, err := decodeValue[[]vmopv1.DynamicDirectPathIODevice](value)
		if err != nil {
			return err
		}
		return checkItems(&v, p.Allowed, p.Denied, matchDirect)
	}
	return nil
}

func decodeValue[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("invalid effective value: %w", err)
	}
	return result, nil
}

func compareNumericPolicy(candidate, ceiling any) error {

	minimum, maximum, _ := bounds(ceiling)
	cmin, cmax, def := bounds(candidate)
	if minimum != nil && (cmin == nil || cmin.Cmp(minimum) < 0) {
		return fmt.Errorf("class minimum is outside governing bounds")
	}
	if maximum != nil && (cmax == nil || cmax.Cmp(maximum) > 0) {
		return fmt.Errorf("class maximum is outside governing bounds")
	}
	return inRange(def, minimum, maximum)
}

func checkNumericValue(value, policy any, path string, minHostMHz int64) error {

	minimum, maximum, _ := bounds(policy)
	if minimum == nil && maximum == nil {
		return nil
	}
	var n *big.Rat
	var err error
	if path == "bootOptions.bootDelay" || path == "bootOptions.bootRetryDelay" {
		if value != nil {
			var d time.Duration
			d, err = time.ParseDuration(fmt.Sprint(value))
			n = new(big.Rat).SetFrac64(int64(d), int64(time.Millisecond))
		}
	} else {
		n, err = rational(value)
	}
	if err != nil {
		return err
	}
	if strings.HasPrefix(path, "resources.limits.") && (n == nil || n.Cmp(big.NewRat(-1, 1)) == 0) {
		if maximum != nil {
			return fmt.Errorf("unlimited allocation exceeds governing maximum")
		}
		return nil
	}
	if path == "resources.requests.cpu" || path == "resources.limits.cpu" {
		if minHostMHz <= 0 {
			return fmt.Errorf("target cluster minimum host frequency is unavailable")
		}
		if minimum != nil {
			minimum.Mul(minimum, new(big.Rat).SetInt64(minHostMHz))
		}
		if maximum != nil {
			maximum.Mul(maximum, new(big.Rat).SetInt64(minHostMHz))
		}
	}
	return inRange(n, minimum, maximum)
}
