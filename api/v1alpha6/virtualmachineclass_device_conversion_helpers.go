// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha6

import (
	"fmt"

	apiconversion "k8s.io/apimachinery/pkg/conversion"
)

// ConvertLegacyClassDevices converts legacy lists into concrete device policies.
// Repeated DirectPath identities are grouped in their first-seen order, with
// labels included in each identity. Nil lists leave the destination unchanged.
func ConvertLegacyClassDevices[V, D any](
	vgpu []V,
	direct []D,
	out *VirtualDevices,
	s apiconversion.Scope,
	convertVGPU func(*V, *VGPUDevice, apiconversion.Scope) error,
	convertDirect func(*D, *DynamicDirectPathIODevice, apiconversion.Scope) error,
) error {
	if vgpu != nil {
		items := make([]VGPUDevice, len(vgpu))
		if err := convertClassDeviceSlice(vgpu, items, s, convertVGPU); err != nil {
			return err
		}
		out.VGPUDevices = &VGPUDeviceListPolicy{Default: &items}
	}
	if direct != nil {
		items := make([]DynamicDirectPathIODevice, 0, len(direct))
		type identity struct {
			vendor, device int64
			label          string
		}
		indices := map[identity]int{}
		for i := range direct {
			var item DynamicDirectPathIODevice
			if err := convertDirect(&direct[i], &item, s); err != nil {
				return err
			}
			key := identity{item.VendorID, item.DeviceID, item.CustomLabel}
			if index, ok := indices[key]; ok {
				items[index].Count++
			} else {
				indices[key] = len(items)
				items = append(items, item)
			}
		}
		out.DynamicDirectPathIODevices = &DynamicDirectPathIODeviceListPolicy{Default: &items}
	}
	return nil
}

// ProjectClassDevices projects policy defaults into legacy device lists.
// DirectPath counts must be positive and their total must fit within the
// conversion safety limit before allocation. Missing defaults leave the
// destination unchanged; explicitly empty defaults produce empty slices.
func ProjectClassDevices[V, D any](
	in *VirtualDevices,
	vgpu *[]V,
	direct *[]D,
	s apiconversion.Scope,
	convertVGPU func(*VGPUDevice, *V, apiconversion.Scope) error,
	convertDirect func(*DynamicDirectPathIODevice, *D, apiconversion.Scope) error,
) error {
	if p := in.VGPUDevices; p != nil && p.Default != nil {
		*vgpu = make([]V, len(*p.Default))
		if err := convertClassDeviceSlice(*p.Default, *vgpu, s, convertVGPU); err != nil {
			return err
		}
	}
	if p := in.DynamicDirectPathIODevices; p != nil && p.Default != nil {
		var count int64
		for _, device := range *p.Default {
			if device.Count < 1 {
				return fmt.Errorf("dynamic DirectPath device count must be positive")
			}
			count += int64(device.Count)
			// Bound conversion work before allocating the legacy representation.
			// This is a conversion safety limit, not a hardware capability limit.
			if count > MaxClassDeviceExpansion {
				return fmt.Errorf("dynamic DirectPath projection exceeds %d legacy devices", MaxClassDeviceExpansion)
			}
		}
		*direct = make([]D, 0, count)
		for _, device := range *p.Default {
			var item D
			if err := convertDirect(&device, &item, s); err != nil {
				return err
			}
			for range device.Count {
				*direct = append(*direct, item)
			}
		}
	}
	return nil
}

func convertClassDeviceSlice[S, D any](
	in []S,
	out []D,
	s apiconversion.Scope,
	convert func(*S, *D, apiconversion.Scope) error,
) error {
	for i := range in {
		if err := convert(&in[i], &out[i], s); err != nil {
			return err
		}
	}
	return nil
}
