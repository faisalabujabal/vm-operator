// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package vmclass

import (
	"context"
	"fmt"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1util "github.com/vmware-tanzu/vm-operator/pkg/util/vmopv1"
)

// applyBootDefaults materializes class-owned boot fields. A class change clears
// fields omitted by the new class while preserving VM-only boot settings.
func applyBootDefaults(vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec, reset bool) {
	hadBootOptions := vm.Spec.BootOptions != nil
	boot := vm.Spec.BootOptions
	if boot == nil {
		boot = &vmopv1.VirtualMachineBootOptions{}
	}
	// These fields belong to the selected class; the remaining VM boot fields
	// keep their existing values.
	if reset {
		boot.EFISecureBoot = ""
		boot.BootRetry = ""
		boot.BootDelay = nil
		boot.BootRetryDelay = nil
	}
	if defaults := spec.BootOptions; defaults != nil {
		if value := vmopv1.ClassBoolDefault(defaults.EFISecureBoot); value != nil {
			boot.EFISecureBoot = vmopv1.VirtualMachineBootOptionsEFISecureBootDisabled
			if *value {
				boot.EFISecureBoot = vmopv1.VirtualMachineBootOptionsEFISecureBootEnabled
			}
		}
		if value := vmopv1.ClassBoolDefault(defaults.BootRetry); value != nil {
			boot.BootRetry = vmopv1.VirtualMachineBootOptionsBootRetryDisabled
			if *value {
				boot.BootRetry = vmopv1.VirtualMachineBootOptionsBootRetryEnabled
			}
		}
		if value := vmopv1.ClassLongDefault(defaults.BootDelay); value != nil {
			boot.BootDelay = &metav1.Duration{Duration: time.Duration(*value) * time.Millisecond}
		}
		if value := vmopv1.ClassLongDefault(defaults.BootRetryDelay); value != nil && boot.BootRetry == vmopv1.VirtualMachineBootOptionsBootRetryEnabled {
			boot.BootRetryDelay = &metav1.Duration{Duration: time.Duration(*value) * time.Millisecond}
		}
	}
	// vSphere retains an inactive retry delay, but the VM API only accepts it
	// while retry is enabled. Preserve the class/provider preset independently.
	if boot.BootRetry != vmopv1.VirtualMachineBootOptionsBootRetryEnabled {
		boot.BootRetryDelay = nil
	}
	if hadBootOptions || !apiequality.Semantic.DeepEqual(boot, &vmopv1.VirtualMachineBootOptions{}) {
		vm.Spec.BootOptions = boot
	}
}

// applyBootFirmwarePrerequisite records the existing effective EFI firmware only
// when a class secure-boot preset needs the VM API's explicit prerequisite.
// General firmware defaulting remains with the existing provider path.
func applyBootFirmwarePrerequisite(ctx context.Context, c ctrlclient.Client, vm *vmopv1.VirtualMachine, spec *vmopv1.VirtualMachineClassSpec) error {
	if spec.BootOptions == nil || vm.Spec.BootOptions == nil || vm.Spec.BootOptions.Firmware != "" {
		return nil
	}
	secure := vmopv1.ClassBoolDefault(spec.BootOptions.EFISecureBoot)
	if secure == nil || !*secure {
		return nil
	}
	var firmware string
	imageRef := vm.Spec.Image
	if imageRef == nil && vm.Spec.ImageName != "" {
		imageRef = &vmopv1.VirtualMachineImageRef{Name: vm.Spec.ImageName}
	}
	if imageRef != nil {
		image, err := vmopv1util.GetImage(ctx, c, *imageRef, vm.Namespace)
		if err != nil {
			return fmt.Errorf("resolve image firmware for class secure boot: %w", err)
		}
		firmware = image.Status.Firmware
	}
	// Match CreateConfigSpec: a nonempty image firmware overrides raw class
	// firmware. In particular, a BIOS image must never be switched to EFI.
	if firmware == "" {
		doc, err := vmopv1.ParseClassConfigSpec(spec.ConfigSpec)
		if err != nil {
			return err
		}
		if _, err := doc.Get("firmware", &firmware); err != nil {
			return err
		}
	}
	if firmware == string(vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI) {
		vm.Spec.BootOptions.Firmware = vmopv1.VirtualMachineBootOptionsFirmwareTypeEFI
	}
	return nil
}
