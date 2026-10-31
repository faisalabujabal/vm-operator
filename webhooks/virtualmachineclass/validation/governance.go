// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
)

func (v validator) validateGovernance(ctx *pkgctx.WebhookRequestContext, class *vmopv1.VirtualMachineClass) (field.ErrorList, error) {
	if !pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM || !class.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	var errs field.ErrorList
	if class.Spec.Governs != nil && class.Spec.Governs.ExistingVMs == "" {
		errs = append(errs, field.Required(field.NewPath("spec", "governs", "existingVMs"), "required on a governing class without a parent"))
	}
	if class.Spec.Zones != nil && len(*class.Spec.Zones) == 0 {
		return errs, nil
	}
	var classes vmopv1.VirtualMachineClassList
	if err := v.reader.List(ctx, &classes, ctrlclient.InNamespace(class.Namespace)); err != nil {
		return nil, fmt.Errorf("list classes for governance validation in namespace %s: %w", class.Namespace, err)
	}
	// The candidate replaces its stored version on update. Admission reads
	// are namespace-local for both provider-system classes and tenant copies.
	classes.Items = slices.DeleteFunc(classes.Items, func(peer vmopv1.VirtualMachineClass) bool {
		return peer.Namespace != class.Namespace || peer.Name == class.Name || !peer.DeletionTimestamp.IsZero() || peer.Spec.Governs == nil
	})
	slices.SortFunc(classes.Items, func(a, b vmopv1.VirtualMachineClass) int { return strings.Compare(a.Name, b.Name) })
	// A pre-existing conflict can come from gate-off writes or concurrent
	// admissions. Fail closed rather than validating against two ceilings.
	for i := range classes.Items {
		left := &classes.Items[i]
		withinCandidate := intersectZoneScopes(class.Spec.Zones, governingZones(left))
		for j := i + 1; j < len(classes.Items); j++ {
			right := &classes.Items[j]
			if scope, overlaps := overlappingZones(withinCandidate, governingZones(right)); overlaps {
				errs = append(errs, field.Invalid(field.NewPath("spec", "zones"), class.Spec.Zones,
					fmt.Sprintf("ambiguous governance: classes %q and %q govern %s", left.Name, right.Name, scope)))
			}
		}
	}
	if len(errs) > 0 {
		return errs, nil
	}
	for i := range classes.Items {
		ceiling := &classes.Items[i]
		ceilingZones := governingZones(ceiling)
		if class.Spec.Governs != nil {
			if scope, overlaps := overlappingZones(governingZones(class), ceilingZones); overlaps {
				errs = append(errs, field.Invalid(field.NewPath("spec", "governs", "zones"), class.Spec.Governs.Zones,
					fmt.Sprintf("overlaps governing class %q in %s", ceiling.Name, scope)))
				continue
			}
		}
		if scope, overlaps := overlappingZones(class.Spec.Zones, ceilingZones); overlaps {
			for _, violation := range governance.CheckClass(&class.Spec, &ceiling.Spec) {
				violation.Detail = fmt.Sprintf("governing class %q in %s: %s", ceiling.Name, scope, violation.Detail)
				errs = append(errs, violation)
			}
		}
	}
	return errs, nil
}

// governingZones relies on the schema's governs.zones subset rule. An omitted
// governance list inherits availability; nil availability covers future zones.
func governingZones(class *vmopv1.VirtualMachineClass) *[]string {
	if class.Spec.Governs.Zones != nil {
		return class.Spec.Governs.Zones
	}
	return class.Spec.Zones
}

// overlappingZones compares scopes without requiring currently copied Zones.
// Nil is universal, whereas an explicitly empty slice intersects nothing.
func overlappingZones(a, b *[]string) (string, bool) {
	names := intersectZoneScopes(a, b)
	if names == nil {
		return "all namespace zones", true
	}
	return "zones [" + strings.Join(*names, ", ") + "]", len(*names) > 0
}

func intersectZoneScopes(a, b *[]string) *[]string {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		a, b = b, a
	}
	names := []string{}
	for _, name := range *a {
		if b == nil || slices.Contains(*b, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	return &names
}
