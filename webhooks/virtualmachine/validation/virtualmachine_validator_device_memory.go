// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation/field"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

func (v validator) validateDeviceMemoryReservation(ctx *pkgctx.WebhookRequestContext, vm, oldVM *vmopv1.VirtualMachine) field.ErrorList {
	if !pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM || ctx.IsVMOperatorAccount ||
		(oldVM != nil && apiequality.Semantic.DeepEqual(vm.Spec, oldVM.Spec)) {
		return nil
	}
	satisfied, err := vmclass.DeviceMemoryReservationSatisfied(ctx, v.client, vm, oldVM)
	path := field.NewPath("spec", "resources", "requests", "memory")
	if err != nil {
		return field.ErrorList{field.InternalError(path, err)}
	}
	if satisfied {
		return nil
	}
	return field.ErrorList{field.Forbidden(path, "vGPU and DirectPath I/O devices require full guest memory reservation: set spec.memoryAdvanced.reservationLockedToMax=true, or set spec.resources.requests.memory equal to spec.resources.size.memory")}
}
