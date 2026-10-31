// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// DeviceMemorySpec checks real admission without provisioning passthrough hardware.
func DeviceMemorySpec(ctx context.Context, inputGetter func() VMSpecInput) {
	It("requires full memory reservation for passthrough devices and preserves fixed legacy classes", Label("core-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		client := proxy.GetClient()
		var source vmopv1.VirtualMachine
		Expect(client.Get(ctx, ctrlclient.ObjectKey{Namespace: input.WCPNamespaceName, Name: input.LinuxVMName}, &source)).To(Succeed())
		template := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "device-memory-" + uuid.NewString(), Namespace: input.WCPNamespaceName}, Spec: vmopv1.VirtualMachineSpec{Image: source.Spec.Image, ImageName: source.Spec.ImageName, StorageClass: source.Spec.StorageClass, PowerState: vmopv1.VirtualMachinePowerStateOff, Resources: &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse("2")), Memory: ptr.To(resource.MustParse("2Gi"))}}}}
		for _, devices := range []*vmopv1.VirtualMachineDevicesSpec{
			{VGPUDevices: []vmopv1.VirtualMachineVGPUDevice{{ProfileName: "admission-test", Count: 1}}},
			{DynamicDirectPathIODevices: []vmopv1.VirtualMachineDynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, Count: 1}}},
		} {
			vm := template.DeepCopy()
			vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: devices}
			err := client.Create(ctx, vm, ctrlclient.DryRunAll)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("devices require full guest memory reservation"))
			vm = template.DeepCopy()
			vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: devices}
			vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(true)}
			Expect(client.Create(ctx, vm, ctrlclient.DryRunAll)).To(Succeed())
			vm = template.DeepCopy()
			vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: devices}
			vm.Spec.Resources.Requests = &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("2048Mi"))}
			Expect(client.Create(ctx, vm, ctrlclient.DryRunAll)).To(Succeed())
		}
		By("keeping the legacy fixed-class full-reservation path")
		class := &vmopv1a5.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: template.Name, Namespace: input.WCPNamespaceName}}
		class.Spec.Hardware.Cpus = 2
		class.Spec.Hardware.Memory = resource.MustParse("2Gi")
		class.Spec.Hardware.Devices.VGPUDevices = []vmopv1a5.VGPUDevice{{ProfileName: "admission-test"}}
		class.Spec.Policies.Resources.Requests.Memory = resource.MustParse("2Gi")
		Expect(client.Create(ctx, class)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(client.Delete(cleanupCtx, class)).To(Succeed()) })
		vm := template.DeepCopy()
		vm.Spec.ClassName = class.Name
		vm.Spec.Resources = nil
		Expect(client.Create(ctx, vm, ctrlclient.DryRunAll)).To(Succeed())
	})
}
