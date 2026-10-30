// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/common"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/consts"
	"github.com/vmware-tanzu/vm-operator/test/e2e/vmservice/skipper"
)

// VMClassGovernanceSpec verifies governance API and controller behavior.
func VMClassGovernanceSpec(ctx context.Context, inputGetter func() VMClassSchemaUpgradeInput) {
	It("preserves class status and empty governance through older writers", Label("core-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		client := proxy.GetClient()
		class := &vmopv1.VirtualMachineClass{
			ObjectMeta: metav1.ObjectMeta{Namespace: input.WCPNamespaceName, GenerateName: "governance-api-"},
			Spec: vmopv1.VirtualMachineClassSpec{
				Zones:   &[]string{},
				Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &[]string{}, ExistingVMs: "AllowOnViolation", Enforcement: "Deny"},
			},
		}
		Expect(client.Create(ctx, class)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(client.Delete(cleanupCtx, class)).To(Succeed()) })
		key := ctrlclient.ObjectKeyFromObject(class)
		condition := metav1.Condition{Type: "GovernanceTest", Status: metav1.ConditionTrue, Reason: "RoundTrip", LastTransitionTime: metav1.Now()}
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, key, class)).To(Succeed())
			class.Status.Conditions = append(class.Status.Conditions, condition)
			g.Expect(client.Status().Update(ctx, class)).To(Succeed())
		}).Should(Succeed())
		legacy := &vmopv1a5.VirtualMachineClass{}
		Expect(client.Get(ctx, key, legacy)).To(Succeed())
		legacy.Labels = map[string]string{"governance-test": "updated"}
		Expect(client.Update(ctx, legacy)).To(Succeed())
		Expect(client.Get(ctx, key, class)).To(Succeed())
		Expect(class.Status.Conditions).To(ContainElement(HaveField("Type", "GovernanceTest")))
		Expect(class.Spec.Zones).To(HaveValue(BeEmpty()))
		Expect(class.Spec.Governs.Zones).To(HaveValue(BeEmpty()))
		Expect(class.Spec.Governs.ExistingVMs).To(Equal(vmopv1.VirtualMachineClassExistingVMsAllowOnViolation))
	})
	It("reports a selectable class outside a narrowed governing ceiling", Label("core-functional", "experimental"), func() {
		input := inputGetter()
		proxy := input.ClusterProxy.(*common.VMServiceClusterProxy)
		skipper.SkipUnlessInfraIs(input.Config.InfraConfig.InfraName, consts.WCP)
		skipper.SkipUnlessSupervisorCapabilityEnabled(ctx, proxy, consts.VMServiceClasslessVMCapabilityName)
		client := proxy.GetClient()
		// Keep this logical governance fixture isolated from tenant workloads.
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "vmclass-governance-"}}
		Expect(client.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(cleanupCtx context.Context) { Expect(client.Delete(cleanupCtx, namespace)).To(Succeed()) })
		zone := &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: "governance-zone", Namespace: namespace.Name}, Spec: topologyv1.ZoneSpec{Zone: topologyv1.AvailabilityZoneReference{APIVersion: "topology.tanzu.vmware.com/v1alpha1", Name: "governance-fixture"}}}
		Expect(client.Create(ctx, zone)).To(Succeed())
		ceiling := &vmopv1.VirtualMachineClass{
			ObjectMeta: metav1.ObjectMeta{Name: "ceiling", Namespace: namespace.Name},
			Spec: vmopv1.VirtualMachineClassSpec{
				Zones:     &[]string{"governance-zone"},
				Governs:   &vmopv1.VirtualMachineClassGovernance{ExistingVMs: "AllowOnViolation", Enforcement: "Deny"},
				Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1")), Max: ptr.To(resource.MustParse("8")), Default: ptr.To(resource.MustParse("2"))}}},
			},
		}
		class := ceiling.DeepCopy()
		class.Name = "selectable"
		class.Spec.Governs = nil
		class.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("4"))
		Expect(client.Create(ctx, ceiling)).To(Succeed())
		Expect(client.Create(ctx, class)).To(Succeed())
		key := ctrlclient.ObjectKeyFromObject(class)
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, key, class)).To(Succeed())
			g.Expect(class.Status.Zones).To(HaveLen(1))
			condition := apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		}, 2*time.Minute, time.Second).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, ctrlclient.ObjectKeyFromObject(zone), zone)).To(Succeed())
			g.Expect(zone.Status.GoverningClasses).To(Equal([]string{ceiling.Name}))
		}, 2*time.Minute, time.Second).Should(Succeed())
		Expect(client.Get(ctx, ctrlclient.ObjectKeyFromObject(ceiling), ceiling)).To(Succeed())
		ceiling.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("2"))
		Expect(client.Update(ctx, ceiling)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, key, class)).To(Succeed())
			condition := apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Reason).To(Equal(vmopv1.VirtualMachineClassGovernanceViolationReason))
			g.Expect(class.Spec.Resources.Size.CPU.Default.String()).To(Equal("4"))
		}, 2*time.Minute, time.Second).Should(Succeed())
		Expect(client.Delete(ctx, ceiling)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, ctrlclient.ObjectKeyFromObject(zone), zone)).To(Succeed())
			g.Expect(zone.Status.GoverningClasses).To(BeEmpty())
		}, 2*time.Minute, time.Second).Should(Succeed())

	})

}
