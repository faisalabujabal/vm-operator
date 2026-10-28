// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

// ListPolicyDefaults describes the defaults consumed from a concrete list policy.
// API structs remain concrete so their schema can be generated.
type ListPolicyDefaults[D any] interface {
	GetDefault() *[]D
}

// ListDefaults returns a policy's list defaults. A missing default returns
// nil; an explicitly empty default returns a non-nil empty slice. The returned
// slice belongs to the policy and must not be modified by consumers.
func ListDefaults[D any](policy ListPolicyDefaults[D]) []D {
	if policy == nil || policy.GetDefault() == nil {
		return nil
	}
	return *policy.GetDefault()
}
