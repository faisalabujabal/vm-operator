// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"slices"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
)

// ClassStatus evaluates every available zone from one class-list snapshot.
// Explicit availability is reported even if a Zone has not yet been copied;
// hardware compatibility and namespace-association validation are separate.
// Existing unrelated conditions and transition timestamps are preserved.
func ClassStatus(class *vmopv1.VirtualMachineClass, classes []vmopv1.VirtualMachineClass, namespaceZones []string) vmopv1.VirtualMachineClassStatus {
	status := class.Status.DeepCopy()
	names := namespaceZones
	if class.Spec.Zones != nil {
		names = *class.Spec.Zones
	}
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	previous := map[string]vmopv1.VirtualMachineClassZoneStatus{}
	for _, z := range status.Zones {
		previous[z.Name] = z
	}
	status.Zones = nil
	for _, name := range names {
		z := previous[name]
		z.Name = name
		within, governing := metav1.ConditionTrue, metav1.ConditionFalse
		withinReason, governingReason := vmopv1.VirtualMachineClassNoGoverningClassReason, vmopv1.VirtualMachineClassNotGoverningReason
		message := "No governing class applies"
		ceiling, err := ResolveClasses(classes, class.Namespace, name)
		switch {
		case err != nil:
			within = metav1.ConditionFalse
			withinReason = vmopv1.VirtualMachineClassGovernanceConflictReason
			governingReason = withinReason
			message = err.Error()
		case ceiling != nil:
			withinReason = vmopv1.VirtualMachineClassGovernanceSatisfiedReason
			message = "Class fits within the governing ceiling"
			if ceiling.Name == class.Name {
				governing = metav1.ConditionTrue
				governingReason = vmopv1.VirtualMachineClassGovernanceSatisfiedReason
			}
			if violations := CheckClass(&class.Spec, &ceiling.Spec); len(violations) > 0 {
				within = metav1.ConditionFalse
				withinReason = vmopv1.VirtualMachineClassGovernanceViolationReason
				message = violations.ToAggregate().Error()
			}
		}
		apimeta.SetStatusCondition(&z.Conditions, metav1.Condition{Type: vmopv1.VirtualMachineClassConditionWithinGovernance, Status: within, ObservedGeneration: class.Generation, Reason: withinReason, Message: message})
		apimeta.SetStatusCondition(&z.Conditions, metav1.Condition{Type: vmopv1.VirtualMachineClassConditionGoverning, Status: governing, ObservedGeneration: class.Generation, Reason: governingReason, Message: "Governance is resolved from live class specifications"})
		status.Zones = append(status.Zones, z)
	}
	return *status
}
