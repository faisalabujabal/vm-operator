// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"slices"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
)

// VMZoneSelectionSpec exercises installed admission in a dedicated E2E namespace.
// It temporarily governs its real zones and creates only dry-run VMs. Actual DRS
// placement is covered by vcsim; live provisioning coverage remains T504.
func VMZoneSelectionSpec(ctx context.Context, inputGetter func() VMSpecInput) {
	It("uses supplied CPU values to disambiguate eligible zones", Label("extended-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		adminProxy, err := proxy.NewAdminClusterProxy(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { adminProxy.Dispose(ctx) })
		admin, err := adminProxy.GetAdminClient()
		Expect(err).NotTo(HaveOccurred())
		var zones topologyv1.ZoneList
		Expect(admin.List(ctx, &zones, ctrlclient.InNamespace(input.WCPNamespaceName))).To(Succeed())
		var names []string
		for _, zone := range zones.Items {
			if zone.DeletionTimestamp.IsZero() {
				names = append(names, zone.Name)
			}
		}
		if len(names) < 2 {
			Skip("requires at least two real zones in the dedicated E2E namespace")
		}
		slices.Sort(names)
		var classes vmopv1.VirtualMachineClassList
		Expect(admin.List(ctx, &classes, ctrlclient.InNamespace(input.WCPNamespaceName))).To(Succeed())
		for _, class := range classes.Items {
			if class.Spec.Governs != nil {
				Skip("requires a dedicated namespace without existing governing classes")
			}
		}
		client := proxy.GetClient()
		var source vmopv1.VirtualMachine
		Expect(client.Get(ctx, ctrlclient.ObjectKey{Namespace: input.WCPNamespaceName, Name: input.LinuxVMName}, &source)).To(Succeed())
		for i := range 2 {
			selected := names[:1]
			minimum, maximum := "2", "4"
			if i == 1 {
				selected = names[1:]
				minimum, maximum = "5", "8"
			}
			class := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "zone-selection-", Namespace: input.WCPNamespaceName}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &selected, ExistingVMs: vmopv1.VirtualMachineClassExistingVMsAllowOnViolation}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse(minimum)), Max: ptr.To(resource.MustParse(maximum)), Default: ptr.To(resource.MustParse(minimum))}}}}}
			Expect(admin.Create(ctx, class)).To(Succeed())
			DeferCleanup(func(cleanupCtx context.Context) { Expect(admin.Delete(cleanupCtx, class)).To(Succeed()) })
		}
		template := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "zone-selection-" + uuid.NewString(), Namespace: input.WCPNamespaceName}, Spec: vmopv1.VirtualMachineSpec{Image: source.Spec.Image, ImageName: source.Spec.ImageName, StorageClass: source.Spec.StorageClass, PowerState: vmopv1.VirtualMachinePowerStateOff, Resources: &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("2Gi"))}}}}
		err = client.Create(ctx, template.DeepCopy(), ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("zone-dependent fields resources.size.cpu"))
		for _, value := range []string{"2", "6"} {
			vm := template.DeepCopy()
			vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse(value))
			Expect(client.Create(ctx, vm, ctrlclient.DryRunAll)).To(Succeed())
		}
		vm := template.DeepCopy()
		vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse("10"))
		err = client.Create(ctx, vm, ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no eligible zones"))
		// An unsupported class remains writable but cannot be used in that zone.
		hardwareClass := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "incompatible-zone-", Namespace: input.WCPNamespaceName}, Spec: vmopv1.VirtualMachineClassSpec{
			Zones: ptr.To(names[:1]),
		}}
		hardwareClass.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("2"))
		hardwareClass.Spec.Resources.Size.Memory = vmopv1.FixedClassResource(resource.MustParse("2Gi"))
		hardwareClass.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Default: &[]vmopv1.VGPUDevice{{ProfileName: "unavailable-" + uuid.NewString()}}}
		Expect(admin.Create(ctx, hardwareClass)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(admin.Delete(cleanupCtx, hardwareClass)).To(Succeed()) })
		Eventually(func(g Gomega) {
			g.Expect(admin.Get(ctx, ctrlclient.ObjectKeyFromObject(hardwareClass), hardwareClass)).To(Succeed())
			g.Expect(hardwareClass.Status.Zones).To(HaveLen(1))
			condition := apimeta.FindStatusCondition(hardwareClass.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionHardwareCompatible)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.ObservedGeneration).To(Equal(hardwareClass.Generation))
		}, 2*time.Minute, time.Second).Should(Succeed())
		vm = template.DeepCopy()
		vm.Spec.ClassName = hardwareClass.Name
		vm.Labels = map[string]string{corev1.LabelTopologyZone: names[0]}
		vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse("2"))
		vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(true)}
		err = client.Create(ctx, vm, ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("class hardware is incompatible"))

	})
}
