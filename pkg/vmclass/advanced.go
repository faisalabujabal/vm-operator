// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"slices"

	apiequality "k8s.io/apimachinery/pkg/api/equality"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
)

func applyAdvancedDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, reset bool) bool {
	before := vm.Spec.Advanced.DeepCopy()
	if vm.Spec.Advanced == nil {
		vm.Spec.Advanced = &vmopv1.VirtualMachineAdvancedSpec{}
	}
	d := vm.Spec.Advanced
	a := spec.Advanced
	if a == nil {
		a = &vmopv1.VirtualMachineClassAdvancedSpec{}
	}
	if reset || (a.ChangeBlockTracking != nil && a.ChangeBlockTracking.Default != nil) {
		d.ChangeBlockTracking = vmopv1.ClassBoolDefault(a.ChangeBlockTracking)
	}
	if reset || (a.PreferHTEnabled != nil && a.PreferHTEnabled.Default != nil) {
		d.PreferHTEnabled = vmopv1.ClassBoolDefault(a.PreferHTEnabled)
	}
	if reset || (a.HugePages1GEnabled != nil && a.HugePages1GEnabled.Default != nil) {
		d.HugePages1GEnabled = vmopv1.ClassBoolDefault(a.HugePages1GEnabled)
	}
	if reset || (a.TimeTrackerLowLatencyEnabled != nil && a.TimeTrackerLowLatencyEnabled.Default != nil) {
		d.TimeTrackerLowLatencyEnabled = vmopv1.ClassBoolDefault(a.TimeTrackerLowLatencyEnabled)
	}
	if reset || (a.CPUAffinityExclusiveNoStatsEnabled != nil && a.CPUAffinityExclusiveNoStatsEnabled.Default != nil) {
		d.CPUAffinityExclusiveNoStatsEnabled = vmopv1.ClassBoolDefault(a.CPUAffinityExclusiveNoStatsEnabled)
	}
	if reset || (a.VMXSwapEnabled != nil && a.VMXSwapEnabled.Default != nil) {
		d.VMXSwapEnabled = vmopv1.ClassBoolDefault(a.VMXSwapEnabled)
	}
	if reset || (a.PNUMANodeAffinity != nil && a.PNUMANodeAffinity.Default != nil) {
		d.PNUMANodeAffinity = nil
	}
	if a.PNUMANodeAffinity != nil && a.PNUMANodeAffinity.Default != nil {
		d.PNUMANodeAffinity = slices.Clone(*a.PNUMANodeAffinity.Default)
	}
	if reset || (a.ExtraConfig != nil && a.ExtraConfig.Default != nil) {
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
	// Preserve explicit empty values in policy defaults without sharing storage.
	if a.ExtraConfig != nil && a.ExtraConfig.Default != nil && d.ExtraConfig == nil {
		d.ExtraConfig = []vmopv1common.KeyValuePair{}
	}
	if apiequality.Semantic.DeepEqual(d, &vmopv1.VirtualMachineAdvancedSpec{}) {
		vm.Spec.Advanced = nil
	}
	return !apiequality.Semantic.DeepEqual(before, vm.Spec.Advanced)
}
