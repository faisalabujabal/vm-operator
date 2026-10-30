// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"
	"path"
	"regexp"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
)

func matchString(value string, matcher *vmopv1.StringMatcher) (bool, error) {
	if matcher == nil {
		return true, nil
	}
	modes := 0
	if matcher.Fixed != nil {
		modes++
	}
	if matcher.Glob != nil {
		modes++
	}
	if matcher.Regex != nil {
		modes++
	}
	if modes != 1 {
		return false, fmt.Errorf("governing string matcher must have exactly one mode")
	}
	if matcher.Fixed != nil {
		return value == *matcher.Fixed, nil
	}
	if matcher.Glob != nil {
		matched, err := path.Match(*matcher.Glob, value)
		if err != nil {
			return false, fmt.Errorf("invalid governing glob: %w", err)
		}
		return matched, nil
	}
	matched, err := regexp.MatchString(*matcher.Regex, value)
	if err != nil {
		return false, fmt.Errorf("invalid governing regular expression: %w", err)
	}
	return matched, nil
}

func matchExtra(item vmopv1common.KeyValuePair, m vmopv1.ExtraConfigMatcher) (bool, error) {
	return matchString(item.Key, m.Key)
}
func matchVGPU(item vmopv1.VGPUDevice, m vmopv1.VGPUDeviceMatcher) (bool, error) {
	return matchString(item.ProfileName, m.ProfileName)
}
func matchDirect(item vmopv1.DynamicDirectPathIODevice, m vmopv1.DynamicDirectPathIODeviceMatcher) (bool, error) {
	label, err := matchString(item.CustomLabel, m.CustomLabel)
	return label && (m.VendorID == nil || *m.VendorID == item.VendorID) &&
		(m.DeviceID == nil || *m.DeviceID == item.DeviceID), err
}

func checkItems[T, M any](items *[]T, allowed, denied *[]M, matches func(T, M) (bool, error)) error {
	// Validate every matcher even for an empty list or after an earlier match.
	// Invalid policy must not become an accidental allow on another call path.
	var zero T
	for _, rules := range []*[]M{allowed, denied} {
		if rules != nil {
			for _, m := range *rules {
				if _, err := matches(zero, m); err != nil {
					return err
				}
			}
		}
	}
	if items == nil {
		return nil
	}
	for index, item := range *items {
		if denied != nil {
			for _, m := range *denied {
				match, err := matches(item, m)
				if err != nil {
					return err
				}
				if match {
					return fmt.Errorf("item %d is denied by governance", index)
				}
			}
		}
		if allowed == nil {
			continue
		}
		permitted := false
		for _, m := range *allowed {
			match, err := matches(item, m)
			if err != nil {
				return err
			}
			permitted = permitted || match
		}
		if !permitted {
			return fmt.Errorf("item %d is not allowed by governance", index)
		}
	}
	return nil
}
