// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"slices"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation/field"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
)

func applyAdvancedDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, reset bool) {
	if vm.Spec.Advanced == nil {
		vm.Spec.Advanced = &vmopv1.VirtualMachineAdvancedSpec{}
	}
	d := vm.Spec.Advanced
	a := spec.Advanced
	if a == nil {
		a = &vmopv1.VirtualMachineClassAdvancedSpec{}
	}
	applyBoolDefault(&d.ChangeBlockTracking, a.ChangeBlockTracking, reset)
	applyBoolDefault(&d.PreferHTEnabled, a.PreferHTEnabled, reset)
	applyBoolDefault(&d.HugePages1GEnabled, a.HugePages1GEnabled, reset)
	applyBoolDefault(&d.TimeTrackerLowLatencyEnabled, a.TimeTrackerLowLatencyEnabled, reset)
	applyBoolDefault(&d.CPUAffinityExclusiveNoStatsEnabled, a.CPUAffinityExclusiveNoStatsEnabled, reset)
	applyBoolDefault(&d.VMXSwapEnabled, a.VMXSwapEnabled, reset)
	if reset {
		d.PNUMANodeAffinity = nil
	}
	if a.PNUMANodeAffinity != nil && a.PNUMANodeAffinity.Default != nil {
		d.PNUMANodeAffinity = slices.Clone(*a.PNUMANodeAffinity.Default)
	}
	if reset {
		d.ExtraConfig = nil
	}
	if a.ExtraConfig != nil && a.ExtraConfig.Default != nil {
		d.ExtraConfig = []vmopv1common.KeyValuePair{}
		typedKeys := vmopv1util.AdvancedVMXKeyMap()
		for _, item := range *a.ExtraConfig.Default {
			if _, typed := typedKeys[item.Key]; typed || vmopv1util.IsSystemReservedExtraConfigKey(item.Key) || vmopv1util.IsEthernetDeviceKey(item.Key) {
				continue
			}
			d.ExtraConfig = append(d.ExtraConfig, item)
		}
	}

	if apiequality.Semantic.DeepEqual(d, &vmopv1.VirtualMachineAdvancedSpec{}) {
		vm.Spec.Advanced = nil
	}
}

// ValidateAdvancedPolicies validates advanced class policy relationships that
// cannot be expressed within the CRD's CEL evaluation budget.
func ValidateAdvancedPolicies(spec *vmopv1.VirtualMachineClassSpec) field.ErrorList {
	if spec.Advanced == nil {
		return nil
	}
	var errors field.ErrorList

	policy := spec.Advanced.PNUMANodeAffinity
	if policy == nil || policy.Default == nil || policy.Allowed == nil {
		return errors
	}
	defaults := slices.Clone(*policy.Default)
	slices.Sort(defaults)
	for _, set := range *policy.Allowed {
		members := slices.Clone(set.Members)
		slices.Sort(members)
		if slices.Equal(defaults, members) {
			return errors
		}
	}
	return append(errors, field.Invalid(field.NewPath("spec", "advanced", "pnumaNodeAffinity", "default"), *policy.Default, "default must be an allowed set"))
}
