// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
)

// VMClassDeviceConversionSpec tests the served class device API without
// allocating hardware. Actual VM PCI realization needs a compatible GPU host.
func VMClassDeviceConversionSpec(ctx context.Context, inputGetter func() VMClassSchemaUpgradeInput) {
	It("preserves device policies through older API reads and writes", Label("core-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		client := proxy.GetClient()
		vgpu := []vmopv1.VGPUDevice{{ProfileName: "conversion-fixture"}}
		direct := []vmopv1.DynamicDirectPathIODevice{
			{VendorID: 1, DeviceID: 2, CustomLabel: "same", Count: 2},
			{VendorID: 1, DeviceID: 2, CustomLabel: "distinct", Count: 1},
		}
		pattern := "conversion-*"
		allowed := []vmopv1.VGPUDeviceMatcher{{ProfileName: &vmopv1.StringMatcher{Glob: &pattern}}}
		class := &vmopv1.VirtualMachineClass{
			ObjectMeta: metav1.ObjectMeta{Namespace: input.WCPNamespaceName, GenerateName: "device-conversion-"},
			Spec: vmopv1.VirtualMachineClassSpec{
				Resources: vmopv1.VirtualMachineClassResources{
					Requests: vmopv1.VirtualMachineClassResourceSpec{Memory: &vmopv1.ResourceQuantityRange{Default: resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)}},
					Size: vmopv1.VirtualMachineClassResourceSpec{
						CPU:    &vmopv1.ResourceQuantityRange{Default: resource.NewQuantity(2, resource.DecimalSI)},
						Memory: &vmopv1.ResourceQuantityRange{Default: resource.NewQuantity(2*1024*1024*1024, resource.BinarySI)},
					}},
				Hardware: vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{
					VGPUDevices:                &vmopv1.VGPUDeviceListPolicy{Default: &vgpu, Allowed: &allowed},
					DynamicDirectPathIODevices: &vmopv1.DynamicDirectPathIODeviceListPolicy{Default: &direct},
				}},
			},
		}
		By("creating a typed device class through the API server")
		Expect(client.Create(ctx, class)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(client.Delete(cleanupCtx, class)).To(Succeed()) })
		key := ctrlclient.ObjectKeyFromObject(class)
		legacy := &vmopv1a5.VirtualMachineClass{}
		Expect(client.Get(ctx, key, legacy)).To(Succeed())
		Expect(legacy.Spec.Hardware.Devices.VGPUDevices).To(Equal([]vmopv1a5.VGPUDevice{{ProfileName: "conversion-fixture"}}))
		Expect(legacy.Spec.Hardware.Devices.DynamicDirectPathIODevices).To(Equal([]vmopv1a5.DynamicDirectPathIODevice{
			{VendorID: 1, DeviceID: 2, CustomLabel: "same"},
			{VendorID: 1, DeviceID: 2, CustomLabel: "same"},
			{VendorID: 1, DeviceID: 2, CustomLabel: "distinct"},
		}))
		By("preserving policies when an older writer edits unrelated metadata")
		originalDevices := class.Spec.Hardware.Devices.DeepCopy()
		legacy.Labels = map[string]string{"conversion-test": "updated"}
		Expect(client.Update(ctx, legacy)).To(Succeed())
		Expect(client.Get(ctx, key, class)).To(Succeed())
		Expect(class.Spec.Hardware.Devices).To(Equal(*originalDevices))
		By("honoring an older writer's device edit")
		Expect(client.Get(ctx, key, legacy)).To(Succeed())
		legacy.Spec.Hardware.Devices.VGPUDevices = []vmopv1a5.VGPUDevice{{ProfileName: "edited-fixture"}}
		Expect(client.Update(ctx, legacy)).To(Succeed())
		Expect(client.Get(ctx, key, class)).To(Succeed())
		Expect(*class.Spec.Hardware.Devices.VGPUDevices.Default).To(Equal([]vmopv1.VGPUDevice{{ProfileName: "edited-fixture"}}))
		Expect(class.Spec.Hardware.Devices.VGPUDevices.Allowed).To(BeNil())
		Expect(class.Spec.Hardware.Devices.DynamicDirectPathIODevices).To(Equal(originalDevices.DynamicDirectPathIODevices))
	})
}
