// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"fmt"

	vimtypes "github.com/vmware/govmomi/vim25/types"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/providers/vsphere/constants"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

const (
	// A negative device range is traditionally used.
	pciDevicesStartDeviceKey      = int32(-200)
	instanceStorageStartDeviceKey = int32(-300)
)

func CreatePCIPassThroughDevice(deviceKey int32, backingInfo vimtypes.BaseVirtualDeviceBackingInfo) vimtypes.BaseVirtualDevice {
	device := &vimtypes.VirtualPCIPassthrough{
		VirtualDevice: vimtypes.VirtualDevice{
			Key:     deviceKey,
			Backing: backingInfo,
		},
	}
	return device
}

// CreatePCIDevicesFromConfigSpec creates vim25 VirtualDevices from the specified list of PCI devices from the VM Class ConfigSpec.
func CreatePCIDevicesFromConfigSpec(pciDevsFromConfigSpec []*vimtypes.VirtualPCIPassthrough) []vimtypes.BaseVirtualDevice {
	devices := make([]vimtypes.BaseVirtualDevice, 0, len(pciDevsFromConfigSpec))

	deviceKey := pciDevicesStartDeviceKey

	for i := range pciDevsFromConfigSpec {
		dev := pciDevsFromConfigSpec[i]
		dev.Key = deviceKey
		devices = append(devices, dev)
		deviceKey--
	}

	return devices
}

// CreatePCIDevicesFromVMClass creates vim25 VirtualDevices from the specified list of PCI devices from VM Class spec.
func CreatePCIDevicesFromVMClass(pciDevicesFromVMClass vmopv1.VirtualDevices) ([]vimtypes.BaseVirtualDevice, error) {
	var vgpuDevices []vmopv1.VGPUDevice
	if p := pciDevicesFromVMClass.VGPUDevices; p != nil && p.Default != nil {
		vgpuDevices = *p.Default
	}
	var dynamicDevices []vmopv1.DynamicDirectPathIODevice
	if p := pciDevicesFromVMClass.DynamicDirectPathIODevices; p != nil && p.Default != nil {
		dynamicDevices = *p.Default
	}
	deviceCount := int64(0)
	for range vgpuDevices {
		if deviceCount+1 > vmopv1.MaxClassDeviceExpansion {
			return nil, fmt.Errorf("VM Class device expansion count exceeds maximum %d", vmopv1.MaxClassDeviceExpansion)
		}
		deviceCount++
	}
	for _, device := range dynamicDevices {
		if device.Count < 1 {
			return nil, fmt.Errorf("DirectPath I/O device %d:%d count must be positive", device.VendorID, device.DeviceID)
		}
		if deviceCount+int64(device.Count) > vmopv1.MaxClassDeviceExpansion {
			return nil, fmt.Errorf("VM Class device expansion count exceeds maximum %d", vmopv1.MaxClassDeviceExpansion)
		}
		deviceCount += int64(device.Count)
	}
	devices := make([]vimtypes.BaseVirtualDevice, 0, int(deviceCount))

	deviceKey := pciDevicesStartDeviceKey

	for _, vGPU := range vgpuDevices {
		backingInfo := &vimtypes.VirtualPCIPassthroughVmiopBackingInfo{
			Vgpu: vGPU.ProfileName,
		}
		dev := CreatePCIPassThroughDevice(deviceKey, backingInfo)
		devices = append(devices, dev)
		deviceKey--
	}

	for _, dynamicDirectPath := range dynamicDevices {
		allowedDev := vimtypes.VirtualPCIPassthroughAllowedDevice{
			VendorId: int32(dynamicDirectPath.VendorID), //nolint:gosec // disable G115
			DeviceId: int32(dynamicDirectPath.DeviceID), //nolint:gosec // disable G115
		}
		backingInfo := &vimtypes.VirtualPCIPassthroughDynamicBackingInfo{
			AllowedDevice: []vimtypes.VirtualPCIPassthroughAllowedDevice{allowedDev},
			CustomLabel:   dynamicDirectPath.CustomLabel,
		}
		for range dynamicDirectPath.Count {
			dev := CreatePCIPassThroughDevice(deviceKey, backingInfo)
			devices = append(devices, dev)
			deviceKey--
		}
	}

	return devices, nil
}

func CreateInstanceStorageDiskDevices(isVolumes []vmopv1.VirtualMachineVolume) []vimtypes.BaseVirtualDevice {
	devices := make([]vimtypes.BaseVirtualDevice, 0, len(isVolumes))
	deviceKey := instanceStorageStartDeviceKey

	for _, volume := range isVolumes {
		device := &vimtypes.VirtualDisk{
			CapacityInBytes: volume.PersistentVolumeClaim.InstanceVolumeClaim.Size.Value(),
			VirtualDevice: vimtypes.VirtualDevice{
				Key: deviceKey,
				Backing: &vimtypes.VirtualDiskFlatVer2BackingInfo{
					ThinProvisioned: ptr.To(false),
				},
			},
			VDiskId: &vimtypes.ID{
				Id: constants.InstanceStorageVDiskID,
			},
		}
		devices = append(devices, device)
		deviceKey--
	}

	return devices
}
