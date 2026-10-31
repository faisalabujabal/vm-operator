// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"context"
	"fmt"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
)

// SameClassResizeRequested identifies the opt-in resize path that materializes
// the latest class defaults. Class changes retain their existing resize path.
func SameClassResizeRequested(ctx context.Context, vm *vmopv1.VirtualMachine, class *vmopv1.VirtualMachineClass) bool {
	features := pkgcfg.FromContext(ctx).Features
	if !features.VMServiceClasslessVM || !features.VMResizeCPUMemory || vm.Spec.ClassName == "" {
		return false
	}
	if _, ok := vm.Annotations[vmopv1.VirtualMachineSameVMClassResizeAnnotation]; !ok {
		return false
	}
	if name, _, _, exists := vmopv1util.GetLastResizedAnnotation(*vm); exists && name != vm.Spec.ClassName {
		return false
	}
	return vm.Spec.ClassName == class.Name && vmopv1util.ResizeNeeded(*vm, *class)
}

// RefreshSameClassDefaults persists supported class-supplied fields for an
// explicit same-class resize using the live class selected by the provider.
// The caller's object is never modified: its deferred patch helper must not
// replay this spec patch over a later user edit. Callers must stop reconciliation
// after a successful write and retry from a fresh object after conflicts.
func RefreshSameClassDefaults(ctx context.Context, c ctrlclient.Client, provider ResourceDefaultsProvider, vm *vmopv1.VirtualMachine, class *vmopv1.VirtualMachineClass) (bool, error) {
	if !SameClassResizeRequested(ctx, vm, class) {
		return false, nil
	}
	desired := vm.DeepCopy()
	changed, err := applyResolvedDefaults(ctx, provider, desired, &class.Spec, true)
	if err != nil || !changed {
		return false, err
	}
	if err := c.Patch(ctx, desired, ctrlclient.MergeFromWithOptions(vm.DeepCopy(), ctrlclient.MergeFromWithOptimisticLock{})); err != nil {
		return false, fmt.Errorf("refresh defaults for class %s: %w", class.Name, err)
	}
	return true, nil
}
