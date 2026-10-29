// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

func promoteClassBootOptions(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	boot := &VirtualMachineClassBootOptions{}
	spec.BootOptions = nil
	for _, field := range []struct {
		path string
		out  **BoolPolicy
	}{
		{"bootOptions.efiSecureBootEnabled", &boot.EFISecureBoot},
		{"bootOptions.bootRetryEnabled", &boot.BootRetry},
	} {
		var value bool
		present, err := doc.Get(field.path, &value)
		if err != nil {
			return err
		}
		if present {
			*field.out = FixedClassBool(value)
			if err := doc.Delete(field.path); err != nil {
				return err
			}
		}
	}
	for _, field := range []struct {
		path string
		out  **LongRange
	}{
		{"bootOptions.bootDelay", &boot.BootDelay},
		{"bootOptions.bootRetryDelay", &boot.BootRetryDelay},
	} {
		var value int64
		present, err := doc.Get(field.path, &value)
		if err != nil {
			return err
		}
		if present {
			if value < 0 || value > maxClassBootDelayMilliseconds {
				// Older APIs accepted arbitrary raw presets. Keep values that
				// cannot be represented by VM durations in the legacy document.
				continue
			}
			*field.out = FixedClassLong(value)
			if err := doc.Delete(field.path); err != nil {
				return err
			}
		}
	}
	if boot.EFISecureBoot != nil || boot.BootRetry != nil || boot.BootDelay != nil || boot.BootRetryDelay != nil {
		spec.BootOptions = boot
	}
	return nil
}

func projectClassBootOptions(spec *VirtualMachineClassSpec, doc *ClassConfigSpecDocument) error {
	boot := spec.BootOptions
	if boot == nil {
		return nil
	}
	for _, field := range []struct {
		path string
		in   *BoolPolicy
	}{
		{"bootOptions.efiSecureBootEnabled", boot.EFISecureBoot},
		{"bootOptions.bootRetryEnabled", boot.BootRetry},
	} {
		if value := ClassBoolDefault(field.in); value != nil {
			if err := doc.Set(field.path, *value); err != nil {
				return err
			}
		}
	}
	for _, field := range []struct {
		path string
		in   *LongRange
	}{
		{"bootOptions.bootDelay", boot.BootDelay},
		{"bootOptions.bootRetryDelay", boot.BootRetryDelay},
	} {
		if value := ClassLongDefault(field.in); value != nil {
			if err := doc.Set(field.path, *value); err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreClassBootOptions(dst, preserved, projected *VirtualMachineClassSpec) {
	if preserved.BootOptions == nil {
		return
	}
	if dst.BootOptions == nil && projected.BootOptions == nil {
		dst.BootOptions = preserved.BootOptions
		return
	}
	wasAbsent := dst.BootOptions == nil
	if dst.BootOptions == nil {
		dst.BootOptions = &VirtualMachineClassBootOptions{}
	}
	projection := projected.BootOptions
	if projection == nil {
		projection = &VirtualMachineClassBootOptions{}
	}
	restoreUnchangedClassPolicy(&dst.BootOptions.EFISecureBoot, preserved.BootOptions.EFISecureBoot, projection.EFISecureBoot)
	restoreUnchangedClassPolicy(&dst.BootOptions.BootRetry, preserved.BootOptions.BootRetry, projection.BootRetry)
	restoreUnchangedClassPolicy(&dst.BootOptions.BootDelay, preserved.BootOptions.BootDelay, projection.BootDelay)
	restoreUnchangedClassPolicy(&dst.BootOptions.BootRetryDelay, preserved.BootOptions.BootRetryDelay, projection.BootRetryDelay)
	if wasAbsent && dst.BootOptions.EFISecureBoot == nil && dst.BootOptions.BootRetry == nil &&
		dst.BootOptions.BootDelay == nil && dst.BootOptions.BootRetryDelay == nil {
		dst.BootOptions = nil
	}
}

const maxClassBootDelayMilliseconds = int64(9223372036854)
