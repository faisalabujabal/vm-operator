// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
)

func (v validator) validateZoneSelection(ctx *pkgctx.WebhookRequestContext, vm, oldVM *vmopv1.VirtualMachine) field.ErrorList {
	if !pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM || ctx.IsVMOperatorAccount {
		return nil
	}
	selected := vm.Labels[corev1.LabelTopologyZone]
	if oldVM != nil {
		if !oldVM.DeletionTimestamp.IsZero() || (apiequality.Semantic.DeepEqual(zoneConfiguration(vm.Spec), zoneConfiguration(oldVM.Spec)) && selected == oldVM.Labels[corev1.LabelTopologyZone]) {
			return nil
		}
		// Placement status from the old object is authoritative, not user-supplied status.
		if oldVM.Status.Zone != "" {
			if selected != "" && selected != oldVM.Status.Zone {
				return field.ErrorList{field.Forbidden(field.NewPath("metadata", "labels").Key(corev1.LabelTopologyZone), "an already placed VM must remain in its assigned zone")}
			}
			selected = oldVM.Status.Zone
		}
	}
	path := field.NewPath("metadata", "labels").Key(corev1.LabelTopologyZone)
	var zones topologyv1.ZoneList
	if err := v.zoneReader.List(ctx, &zones, ctrlclient.InNamespace(vm.Namespace)); err != nil {
		return field.ErrorList{field.InternalError(path, err)}
	}
	var names []string
	for _, zone := range zones.Items {
		if zone.DeletionTimestamp.IsZero() && (selected == "" || zone.Name == selected) {
			names = append(names, zone.Name)
		}
	}
	// Namespace Zone copies may not exist yet on older/delayed-copy flows. The
	// existing availability-zone validator retains responsibility for their API.
	if len(names) == 0 && len(zones.Items) == 0 {
		return nil
	}
	result, err := governance.ResolveZones(ctx, v.zoneReader, vm, names)
	if err == nil {
		err = result.Error()
	}
	if err != nil {
		return field.ErrorList{field.Forbidden(path, err.Error())}
	}
	return nil
}

// Unrelated edits (notably powering off a drifted VM) must not turn routing
// validation into the separate existing-VM compliance enforcement policy.
func zoneConfiguration(spec vmopv1.VirtualMachineSpec) vmopv1.VirtualMachineSpec {
	return vmopv1.VirtualMachineSpec{ClassName: spec.ClassName, Class: spec.Class, Resources: spec.Resources, CPUAdvanced: spec.CPUAdvanced, MemoryAdvanced: spec.MemoryAdvanced, BootOptions: spec.BootOptions, Advanced: spec.Advanced, Hardware: spec.Hardware, ReservedProfileName: spec.ReservedProfileName}
}
