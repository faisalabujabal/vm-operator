// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// VirtualMachineClassConditionWithinGovernance reports whether a class fits the zone ceiling.
	VirtualMachineClassConditionWithinGovernance = "WithinGovernance"
	// VirtualMachineClassConditionGoverning reports whether this class governs the zone.
	VirtualMachineClassConditionGoverning = "Governing"
	// VirtualMachineClassConditionReady reports successful class reconciliation.
	VirtualMachineClassConditionReady = "Ready"
	// VirtualMachineClassGovernanceSatisfiedReason indicates a successful evaluation.
	VirtualMachineClassGovernanceSatisfiedReason = "GovernanceSatisfied"
	// VirtualMachineClassGovernanceViolationReason indicates a configuration outside the ceiling.
	VirtualMachineClassGovernanceViolationReason = "GovernanceViolation"
	// VirtualMachineClassGovernanceConflictReason indicates multiple governing classes.
	VirtualMachineClassGovernanceConflictReason = "MultipleGoverningClasses"
	// VirtualMachineClassNoGoverningClassReason indicates no governing class was found.
	VirtualMachineClassNoGoverningClassReason = "NoGoverningClass"
	// VirtualMachineClassNotGoverningReason indicates a selectable, non-governing class.
	VirtualMachineClassNotGoverningReason = "NotGoverning"
	// VirtualMachineClassReconcileErrorReason indicates an incomplete reconciliation.
	VirtualMachineClassReconcileErrorReason = "ReconcileError"
)

// VirtualMachineClassZoneStatus describes the class in one available zone.
type VirtualMachineClassZoneStatus struct {
	// Name identifies the zone.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Conditions describes governance and compatibility in this zone.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
