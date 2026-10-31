// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
)

// ValidateClassFieldUpdates rejects edits to supported fields supplied by the
// selected class. It compares old and new values, allowing retained drift after
// a class edit. Transitions require a resolvable destination; admission separately
// validates whether the selected class or snapshot may change.
func ValidateClassFieldUpdates(ctx context.Context, c ctrlclient.Client, vm, oldVM *vmopv1.VirtualMachine) (field.ErrorList, error) {
	features := pkgcfg.FromContext(ctx).Features
	if !features.VMServiceClasslessVM || vm.Spec.ClassName == "" {
		return nil, nil
	}
	// Update transitions cannot use creation's class-not-yet-copied exception:
	// mutation cannot canonicalize supplied fields without a destination class.
	if vm.Spec.ClassName != oldVM.Spec.ClassName || (features.ImmutableClasses && features.VMResizeCPUMemory && !apiequality.Semantic.DeepEqual(vm.Spec.Class, oldVM.Spec.Class)) {
		destination := &vmopv1.VirtualMachineClass{}
		if err := c.Get(ctx, ctrlclient.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.ClassName}, destination); err != nil {
			if apierrors.IsNotFound(err) {
				return field.ErrorList{field.Invalid(field.NewPath("spec", "className"), vm.Spec.ClassName, "destination class must exist before changing the selected class")}, nil
			}
			return nil, fmt.Errorf("resolve destination class: %w", err)
		}
		return nil, nil
	}
	oldFields, newFields := supportedClassFields(oldVM), supportedClassFields(vm)
	var changed []string
	for path, oldValue := range oldFields {
		if !apiequality.Semantic.DeepEqual(oldValue, newFields[path]) {
			changed = append(changed, path)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	// Resolve ownership against the old selection, including its immutable
	// snapshot. A missing source must not turn an edit into an unowned field.
	spec, err := resolveClassSpec(ctx, c, oldVM, nil)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, fmt.Errorf("cannot resolve field ownership for class %s: class not found", oldVM.Spec.ClassName)
	}
	owned, err := classSuppliedFields(spec)
	if err != nil {
		return nil, fmt.Errorf("resolve fields supplied by class %s: %w", oldVM.Spec.ClassName, err)
	}
	if pkgcfg.FromContext(ctx).Features.ImmutableClasses && oldVM.Spec.Class != nil && oldVM.Spec.Class.Name != "" {
		// Same-class resize materializes the live class without replacing the old
		// snapshot reference. Protect both sources, including the interval before
		// resize completion records its generation. Newly supplied live fields may
		// therefore become protected before the VM opts in to refreshing them.
		live := &vmopv1.VirtualMachineClass{}
		if err := c.Get(ctx, ctrlclient.ObjectKey{Namespace: oldVM.Namespace, Name: oldVM.Spec.ClassName}, live); err != nil {
			return nil, fmt.Errorf("resolve live class field ownership: %w", err)
		}
		liveOwned, err := classSuppliedFields(&live.Spec)
		if err != nil {
			return nil, fmt.Errorf("resolve live class field ownership: %w", err)
		}
		for path, isOwned := range liveOwned {
			owned[path] = owned[path] || isOwned
		}
	}
	slices.Sort(changed)
	var errs field.ErrorList
	for _, path := range changed {
		if owned[path] {
			errs = append(errs, field.Forbidden(field.NewPath("spec", strings.Split(path, ".")...), "field is supplied by the selected class; remove spec.className before editing where classless VMs are permitted, or select another class"))
		}
	}
	return errs, nil
}

func classSuppliedFields(spec *vmopv1.VirtualMachineClassSpec) (map[string]bool, error) {
	effective := spec.DeepCopy()
	var err error
	effective.ConfigSpec, err = vmopv1.ProjectClassConfigSpec(spec)
	if err != nil {
		return nil, err
	}
	if err := vmopv1.PromoteClassConfigSpec(effective); err != nil {
		return nil, err
	}
	presets := &vmopv1.VirtualMachine{}
	// reset=false avoids treating synthesized reset values (for example absent
	// CPU hot-remove=false) as class-owned defaults.
	applyComputeDefaults(presets, effective, false)
	applyBootDefaults(presets, effective, false)
	applyAdvancedDefaults(presets, effective, false)
	owned := map[string]bool{}
	for path, value := range supportedClassFields(presets) {
		owned[path] = value != nil && !reflect.ValueOf(value).IsZero()
	}
	if b := effective.BootOptions; b != nil && vmopv1.ClassLongDefault(b.BootRetryDelay) != nil {
		owned["bootOptions.bootRetryDelay"] = true
	}
	for _, a := range []*vmopv1.VirtualMachineClassAdvancedSpec{effective.Advanced, spec.Advanced} {
		if a == nil {
			continue
		}
		if a.PNUMANodeAffinity != nil && a.PNUMANodeAffinity.Default != nil {
			owned["advanced.pnumaNodeAffinity"] = true
		}
		if a.ExtraConfig != nil && a.ExtraConfig.Default != nil {
			owned["advanced.extraConfig"] = true
		}
	}
	// Resource promotion deliberately leaves native allocations in ConfigSpec.
	// Presence, including explicit zero, determines ownership; unit conversion
	// and default values are irrelevant when comparing old and new VM values.
	doc, err := vmopv1.ParseClassConfigSpec(spec.ConfigSpec)
	if err != nil {
		return nil, err
	}
	for _, entry := range []struct {
		path, raw string
		policy    *vmopv1.ResourceQuantityRange
	}{
		{"resources.size.cpu", "numCPUs", spec.Resources.Size.CPU},
		{"resources.size.memory", "memoryMB", spec.Resources.Size.Memory},
		{"resources.requests.cpu", "cpuAllocation.reservation", spec.Resources.Requests.CPU},
		{"resources.requests.memory", "memoryAllocation.reservation", spec.Resources.Requests.Memory},
		{"resources.limits.cpu", "cpuAllocation.limit", spec.Resources.Limits.CPU},
		{"resources.limits.memory", "memoryAllocation.limit", spec.Resources.Limits.Memory},
	} {
		var value json.RawMessage
		present, err := doc.Get(entry.raw, &value)
		if err != nil {
			return nil, err
		}
		owned[entry.path] = present || (entry.policy != nil && entry.policy.Default != nil)
	}
	return owned, nil
}

// supportedClassFields retains typed values for semantic quantity equality and
// distinguishes an omitted pointer from a supplied false/zero/empty collection.
func supportedClassFields(vm *vmopv1.VirtualMachine) map[string]any {
	cpu := vm.Spec.CPUAdvanced
	if cpu == nil {
		cpu = &vmopv1.VirtualMachineCPUAdvancedSpec{}
	}
	topology := cpu.Topology
	if topology == nil {
		topology = &vmopv1.VirtualMachineCPUTopologySpec{}
	}
	memory := vm.Spec.MemoryAdvanced
	if memory == nil {
		memory = &vmopv1.VirtualMachineMemoryAdvancedSpec{}
	}
	boot := vm.Spec.BootOptions
	if boot == nil {
		boot = &vmopv1.VirtualMachineBootOptions{}
	}
	advanced := vm.Spec.Advanced
	if advanced == nil {
		advanced = &vmopv1.VirtualMachineAdvancedSpec{}
	}
	resources := vm.Spec.Resources
	if resources == nil {
		resources = &vmopv1.VirtualMachineResourcesSpec{}
	}
	size, requests, limits := resources.Size, resources.Requests, resources.Limits
	if size == nil {
		size = &vmopv1.VirtualMachineResourceQuantity{}
	}
	if requests == nil {
		requests = &vmopv1.VirtualMachineResourceQuantity{}
	}
	if limits == nil {
		limits = &vmopv1.VirtualMachineResourceQuantity{}
	}
	// These collections are a map and a set in the API. Ordering alone does
	// not change their selected values; sort copies to leave inputs untouched.
	extraConfig := slices.Clone(advanced.ExtraConfig)
	slices.SortFunc(extraConfig, func(a, b common.KeyValuePair) int { return cmp.Compare(a.Key, b.Key) })
	affinity := slices.Clone(advanced.PNUMANodeAffinity)
	slices.Sort(affinity)
	return map[string]any{
		"resources.size.cpu": size.CPU, "resources.size.memory": size.Memory,
		"resources.requests.cpu": requests.CPU, "resources.requests.memory": requests.Memory,
		"resources.limits.cpu": limits.CPU, "resources.limits.memory": limits.Memory,
		"cpuAdvanced.hotAddEnabled": cpu.HotAddEnabled, "cpuAdvanced.hotRemoveEnabled": cpu.HotRemoveEnabled,
		"cpuAdvanced.iommuEnabled": cpu.IOMMUEnabled, "cpuAdvanced.nestedHardwareVirtualizationEnabled": cpu.NestedHardwareVirtualizationEnabled,
		"cpuAdvanced.performanceCountersEnabled": cpu.PerformanceCountersEnabled, "cpuAdvanced.latencySensitivity": cpu.LatencySensitivity,
		"cpuAdvanced.topology.coresPerSocket": topology.CoresPerSocket, "cpuAdvanced.topology.vnumaNodeCount": topology.VNUMANodeCount,
		"memoryAdvanced.hotAddEnabled": memory.HotAddEnabled, "memoryAdvanced.reservationLockedToMax": memory.ReservationLockedToMax,
		"bootOptions.efiSecureBoot": boot.EFISecureBoot, "bootOptions.bootRetry": boot.BootRetry,
		"bootOptions.bootDelay": boot.BootDelay, "bootOptions.bootRetryDelay": boot.BootRetryDelay,
		"advanced.changeBlockTracking": advanced.ChangeBlockTracking, "advanced.preferHtEnabled": advanced.PreferHTEnabled,
		"advanced.hugePages1GEnabled": advanced.HugePages1GEnabled, "advanced.timeTrackerLowLatencyEnabled": advanced.TimeTrackerLowLatencyEnabled,
		"advanced.cpuAffinityExclusiveNoStatsEnabled": advanced.CPUAffinityExclusiveNoStatsEnabled, "advanced.vmxSwapEnabled": advanced.VMXSwapEnabled,
		"advanced.pnumaNodeAffinity": affinity, "advanced.extraConfig": extraConfig,
	}
}
