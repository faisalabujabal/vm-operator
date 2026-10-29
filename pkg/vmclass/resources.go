// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
)

// ResourceDefaultsProvider resolves class resources into VM resource units.
type ResourceDefaultsProvider interface {
	GetVirtualMachineClassResources(context.Context, vmopv1.VirtualMachineClassSpec) (*vmopv1.VirtualMachineResourcesSpec, error)
}

// ApplyDefaults materializes class settings on creation or a class
// change. Ordinary updates preserve the VM's selected configuration, even if
// its class has since been edited. Removing the class also retains its values.
func ApplyDefaults(
	ctx context.Context,
	c ctrlclient.Client,
	provider ResourceDefaultsProvider,
	vm, oldVM *vmopv1.VirtualMachine,
) (bool, error) {
	features := pkgcfg.FromContext(ctx).Features
	if !features.TelcoVMServiceAPI || vm.Spec.ClassName == "" {
		return false, nil
	}
	if oldVM != nil {
		if !features.VMResizeCPUMemory ||
			(vm.Spec.ClassName == oldVM.Spec.ClassName &&
				apiequality.Semantic.DeepEqual(vm.Spec.Class, oldVM.Spec.Class)) {
			return false, nil
		}
	}
	spec, err := resolveClassSpec(ctx, c, vm, oldVM)
	if err != nil || spec == nil {
		return false, err
	}
	if provider == nil {
		return false, fmt.Errorf("class resource defaults provider is unavailable")
	}
	resources, err := provider.GetVirtualMachineClassResources(ctx, *spec)
	if err != nil {
		return false, fmt.Errorf("resolve resources for class %s: %w", vm.Spec.ClassName, err)
	}
	// Resolve native raw presets and typed defaults through the same mapping
	// used by conversion and the provider. Work on a copy so policy constraints
	// and the original legacy document remain unchanged in the cached class.
	effective := spec.DeepCopy()
	effective.ConfigSpec, err = vmopv1.ProjectClassConfigSpec(spec)
	if err != nil {
		return false, fmt.Errorf("project defaults for class %s: %w", vm.Spec.ClassName, err)
	}
	if err := vmopv1.PromoteClassConfigSpec(effective); err != nil {
		return false, fmt.Errorf("resolve defaults for class %s: %w", vm.Spec.ClassName, err)
	}
	before := vm.Spec.DeepCopy()
	vm.Spec.Resources = resources.DeepCopy()
	applyComputeDefaults(vm, effective, oldVM != nil)
	applyBootDefaults(vm, effective, oldVM != nil)
	return !apiequality.Semantic.DeepEqual(*before, vm.Spec), nil
}

// resolveClassSpec shares class/snapshot selection between defaulting and
// prerequisite validation, including class changes retaining an old snapshot.
func resolveClassSpec(ctx context.Context, c ctrlclient.Client, vm, oldVM *vmopv1.VirtualMachine) (*vmopv1.VirtualMachineClassSpec, error) {
	features := pkgcfg.FromContext(ctx).Features
	useInstance := features.ImmutableClasses && vm.Spec.Class != nil && vm.Spec.Class.Name != ""
	// A className change must not read a snapshot of the previous class.
	if oldVM != nil && vm.Spec.ClassName != oldVM.Spec.ClassName &&
		apiequality.Semantic.DeepEqual(vm.Spec.Class, oldVM.Spec.Class) {
		useInstance = false
	}
	if useInstance {
		var instance vmopv1.VirtualMachineClassInstance
		key := ctrlclient.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.Class.Name}
		if err := c.Get(ctx, key, &instance); err != nil {
			return nil, fmt.Errorf("resolve class instance %s: %w", key, err)
		}
		return &instance.Spec.VirtualMachineClassSpec, nil
	}
	var class vmopv1.VirtualMachineClass
	key := ctrlclient.ObjectKey{Namespace: vm.Namespace, Name: vm.Spec.ClassName}
	if err := c.Get(ctx, key, &class); err != nil {
		// Existing provisioning flows admit the VM before its class is copied
		// into the namespace. Reconciliation resolves it when it arrives.
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve class %s: %w", key, err)
	}
	return &class.Spec, nil
}
