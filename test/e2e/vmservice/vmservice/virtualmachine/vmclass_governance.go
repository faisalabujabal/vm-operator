// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachine

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
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
}
