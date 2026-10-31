// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"

func resolvedStringMatcher(m *vmopv1.StringMatcher) *vmopv1.VirtualMachineClassResolvedStringMatcher {
	if m == nil {
		return nil
	}
	isolated := m.DeepCopy()
	return &vmopv1.VirtualMachineClassResolvedStringMatcher{Fixed: isolated.Fixed, Glob: isolated.Glob, Regex: isolated.Regex}
}

func resolvedExtraConfigMatchers(matchers []vmopv1.ExtraConfigMatcher) []vmopv1.VirtualMachineClassResolvedExtraConfigMatcher {
	result := make([]vmopv1.VirtualMachineClassResolvedExtraConfigMatcher, len(matchers))
	for i, m := range matchers {
		result[i].Key = resolvedStringMatcher(m.Key)
	}
	return result
}
