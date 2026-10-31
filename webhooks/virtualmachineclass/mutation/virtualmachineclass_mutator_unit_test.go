// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package mutation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"encoding/json"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/test/builder"
	"github.com/vmware-tanzu/vm-operator/webhooks/virtualmachineclass/mutation"
)

func uniTests() {
	Describe(
		"Mutate",
		Label(
			testlabels.Create,
			testlabels.Update,
			testlabels.Delete,
			testlabels.API,
			testlabels.Mutation,
			testlabels.Webhook,
		),
		unitTestsMutating,
	)
}

type unitMutationWebhookContext struct {
	builder.UnitTestContextForMutatingWebhook
	vmClass *vmopv1.VirtualMachineClass
}

func newUnitTestContextForMutatingWebhook() *unitMutationWebhookContext {
	vmClass := builder.DummyVirtualMachineClassGenName()
	obj, err := builder.ToUnstructured(vmClass)
	Expect(err).ToNot(HaveOccurred())

	return &unitMutationWebhookContext{
		UnitTestContextForMutatingWebhook: *suite.NewUnitTestContextForMutatingWebhook(obj),
		vmClass:                           vmClass,
	}
}

func unitTestsMutating() {
	var (
		ctx *unitMutationWebhookContext
	)

	BeforeEach(func() {
		ctx = newUnitTestContextForMutatingWebhook()
	})
	AfterEach(func() {
		ctx = nil
	})

	Describe("VirtualMachineClassMutator should admit updates when object is under deletion", func() {
		Context("when update request comes in while deletion in progress ", func() {
			It("should admit update operation", func() {
				t := metav1.Now()
				ctx.WebhookRequestContext.Obj.SetDeletionTimestamp(&t)
				response := ctx.Mutate(&ctx.WebhookRequestContext)
				Expect(response.Allowed).To(BeTrue())
			})
		})
	})

	DescribeTable("gates class defaults by live capability and operation", func(enabled bool, op admissionv1.Operation, expected bool) {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = enabled })
		ctx.vmClass.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		obj, err := builder.ToUnstructured(ctx.vmClass)
		Expect(err).NotTo(HaveOccurred())
		ctx.Obj, ctx.OldObj = obj, obj.DeepCopy()
		ctx.OldObj.Object["spec"].(map[string]interface{})["description"] = "before edit"
		ctx.RawObj, err = json.Marshal(obj)
		Expect(err).NotTo(HaveOccurred())
		ctx.Op = op
		before, oldBefore := ctx.Obj.DeepCopy(), ctx.OldObj.DeepCopy()
		response := ctx.Mutate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(BeTrue())
		Expect(len(response.Patches) > 0).To(Equal(expected))
		Expect(ctx.Obj).To(Equal(before), "input must not be mutated")
		Expect(ctx.OldObj).To(Equal(oldBefore), "old input must not be mutated")
		if op == admissionv1.Create || op == admissionv1.Update {
			pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = !enabled })
			response = ctx.Mutate(&ctx.WebhookRequestContext)
			Expect(response.Allowed).To(BeTrue())
			Expect(len(response.Patches) > 0).To(Equal(!expected))
		}
	},
		Entry("enabled create", true, admissionv1.Create, true),
		Entry("enabled update", true, admissionv1.Update, true),
		Entry("disabled create", false, admissionv1.Create, false),
		Entry("disabled update", false, admissionv1.Update, false),
		Entry("delete", true, admissionv1.Delete, false),
		Entry("connect", true, admissionv1.Connect, false),
	)

	It("does not default metadata-only updates after capability enablement", func() {
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		ctx.vmClass.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		obj, err := builder.ToUnstructured(ctx.vmClass)
		Expect(err).NotTo(HaveOccurred())
		ctx.Obj, ctx.OldObj = obj, obj.DeepCopy()
		ctx.Obj.SetLabels(map[string]string{"housekeeping": "true"})
		ctx.RawObj, err = json.Marshal(ctx.Obj)
		Expect(err).NotTo(HaveOccurred())
		ctx.Op = admissionv1.Update
		response := ctx.Mutate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(BeTrue())
		Expect(response.Patches).To(BeEmpty())
	})

	Describe("SetControllerName", func() {
		When("an update attempts to set an empty spec.controllerName field to empty", func() {
			It("should not indicate anything was mutated", func() {
				oldObj, newObj := ctx.vmClass.DeepCopy(), ctx.vmClass.DeepCopy()
				oldObj.Spec.ControllerName = ""
				newObj.Spec.ControllerName = ""
				Expect(mutation.SetControllerName(newObj, oldObj)).To(BeFalse())
				Expect(newObj.Spec.ControllerName).To(BeEmpty())
			})
		})
		When("an update attempts to set a non-empty spec.controllerName field to empty", func() {
			It("should preserve the original value", func() {
				oldObj, newObj := ctx.vmClass.DeepCopy(), ctx.vmClass.DeepCopy()
				oldObj.Spec.ControllerName = "hello"
				newObj.Spec.ControllerName = ""
				Expect(mutation.SetControllerName(newObj, oldObj)).To(BeTrue())
				Expect(newObj.Spec.ControllerName).To(Equal("hello"))
			})
		})
		When("an update attempts to set a non-empty spec.controllerName field to a new, non-empty value", func() {
			It("should not mutate the request", func() {
				oldObj, newObj := ctx.vmClass.DeepCopy(), ctx.vmClass.DeepCopy()
				oldObj.Spec.ControllerName = "hello"
				newObj.Spec.ControllerName = "world"
				Expect(mutation.SetControllerName(newObj, oldObj)).To(BeFalse())
				Expect(newObj.Spec.ControllerName).To(Equal("world"))
			})
		})
	})
}
