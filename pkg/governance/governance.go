// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

// Package governance evaluates effective VM configurations and class policies.
// Callers resolve defaults and gate runtime use on VMServiceClasslessVM. This
// package never mutates its inputs, queries vSphere, or reads the Zone mirror.
package governance

import (
	"context"
	"errors"
	"fmt"
	"slices"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
)

// ErrMultipleGoverningClasses indicates an ambiguous ceiling. Callers must not
// select an arbitrary winner or combine the competing policies.
var ErrMultipleGoverningClasses = errors.New("multiple classes govern the zone")

// Available reports whether a class is available in a known namespace zone.
func Available(class *vmopv1.VirtualMachineClass, zone string) bool {
	return class != nil && class.DeletionTimestamp.IsZero() && zone != "" &&
		(class.Spec.Zones == nil || slices.Contains(*class.Spec.Zones, zone))
}

// Governs reports whether a class governs a known namespace zone.
func Governs(class *vmopv1.VirtualMachineClass, zone string) bool {
	return Available(class, zone) && class.Spec.Governs != nil &&
		(class.Spec.Governs.Zones == nil || slices.Contains(*class.Spec.Governs.Zones, zone))
}

// Resolve selects the single live governing class in the namespace. The caller
// must supply a zone belonging to that namespace. No class is represented by nil.
func Resolve(ctx context.Context, reader ctrlclient.Reader, namespace, zone string) (*vmopv1.VirtualMachineClass, error) {
	var classes vmopv1.VirtualMachineClassList
	if err := reader.List(ctx, &classes, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list governing classes in %s: %w", namespace, err)
	}
	return ResolveClasses(classes.Items, namespace, zone)
}

// ResolveClasses selects a ceiling from an already listed snapshot. Its result
// is isolated from the snapshot so callers cannot mutate informer-owned data.
func ResolveClasses(classes []vmopv1.VirtualMachineClass, namespace, zone string) (*vmopv1.VirtualMachineClass, error) {
	if namespace == "" || zone == "" {
		return nil, fmt.Errorf("namespace and zone are required for governance resolution")
	}
	var selected *vmopv1.VirtualMachineClass
	for i := range classes {
		class := &classes[i]
		if class.Namespace != namespace || !Governs(class, zone) {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("zone %s: %w", zone, ErrMultipleGoverningClasses)
		}
		selected = class
	}
	if selected == nil {
		return nil, nil
	}
	return selected.DeepCopy(), nil
}

// GoverningNames returns a sorted, unique reporting mirror, including all
// conflicting governors. It is deliberately not used to resolve governance.
func GoverningNames(classes []vmopv1.VirtualMachineClass, namespace, zone string) []string {
	var names []string
	for i := range classes {
		if classes[i].Namespace == namespace && Governs(&classes[i], zone) {
			names = append(names, classes[i].Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
