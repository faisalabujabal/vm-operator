// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/builder"
)

func intgTests() {
	Describe(
		"Create",
		Label(
			testlabels.Create,
			testlabels.EnvTest,
			testlabels.API,
			testlabels.Validation,
			testlabels.Webhook,
		),
		intgTestsValidateCreate,
	)
	Describe(
		"Delete",
		Label(
			testlabels.Delete,
			testlabels.EnvTest,
			testlabels.API,
			testlabels.Validation,
			testlabels.Webhook,
		),
		intgTestsValidateDelete,
	)
}

type intgValidatingWebhookContext struct {
	builder.IntegrationTestContext
	vmClass *vmopv1.VirtualMachineClass
}

func newIntgValidatingWebhookContext() *intgValidatingWebhookContext {
	ctx := &intgValidatingWebhookContext{
		IntegrationTestContext: *suite.NewIntegrationTestContext(),
	}

	ctx.vmClass = builder.DummyVirtualMachineClassGenName()
	ctx.vmClass.Namespace = ctx.Namespace

	return ctx
}

func intgTestsValidateCreate() {
	var (
		ctx *intgValidatingWebhookContext
	)

	validateCreate := func(expectedAllowed bool, expectedReason string, expectedErr error) {
		vmClass := ctx.vmClass.DeepCopy()

		if !expectedAllowed {
			vmClass.Spec.Resources.Requests.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("2Gi"))}
			vmClass.Spec.Resources.Limits.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1Gi"))}
		}

		err := ctx.Client.Create(ctx, vmClass)
		if expectedAllowed {
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(err).To(HaveOccurred())
		}
		if expectedReason != "" {
			Expect(err.Error()).To(ContainSubstring(expectedReason))
		}
	}

	validateSchema := func(mutator func(*vmopv1.VirtualMachineClass), expectFailure bool) {
		vmClass := ctx.vmClass.DeepCopy()
		mutator(vmClass)
		err := ctx.Client.Create(ctx, vmClass)
		if expectFailure {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	BeforeEach(func() {
		ctx = newIntgValidatingWebhookContext()
	})
	AfterEach(func() {
		ctx = nil
	})

	DescribeTable("zone governance schema", func(specJSON string, invalid bool) {
		// Check both create and update so narrowing availability cannot bypass CEL.
		candidate := ctx.vmClass.DeepCopy()
		Expect(json.Unmarshal([]byte(specJSON), &candidate.Spec)).To(Succeed())
		err := ctx.Client.Create(ctx, candidate)
		if invalid {
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "%v", err)
		} else {
			Expect(err).NotTo(HaveOccurred())
		}
		original := ctx.vmClass.DeepCopy()
		Expect(ctx.Client.Create(ctx, original)).To(Succeed())
		original.Spec = candidate.Spec
		err = ctx.Client.Update(ctx, original)
		if invalid {
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "%v", err)
		} else {
			Expect(err).NotTo(HaveOccurred())
		}
	},
		Entry("no governance", `{}`, false),
		Entry("governance inherits all zones", `{"governs":{}}`, false),
		Entry("governance inherits empty availability", `{"zones":[],"governs":{}}`, false),
		Entry("governance inherits explicit availability", `{"zones":["a"],"governs":{}}`, false),
		Entry("unrestricted availability permits explicit governance", `{"governs":{"zones":["a"]}}`, false),
		Entry("empty governance with unrestricted availability", `{"governs":{"zones":[]}}`, false),
		Entry("both empty", `{"zones":[],"governs":{"zones":[]}}`, false),
		Entry("empty governance within explicit availability", `{"zones":["a"],"governs":{"zones":[]}}`, false),
		Entry("subset", `{"zones":["a","b"],"governs":{"zones":["b"]}}`, false),
		Entry("equal sets in different order", `{"zones":["a","b"],"governs":{"zones":["b","a"]}}`, false),
		Entry("outside availability", `{"zones":["a"],"governs":{"zones":["b"]}}`, true),
		Entry("outside empty availability", `{"zones":[],"governs":{"zones":["a"]}}`, true),
		Entry("duplicate availability", `{"zones":["a","a"]}`, true),
		Entry("duplicate governing zones", `{"governs":{"zones":["a","a"]}}`, true),
		Entry("allow existing violations", `{"governs":{"enforcement":"Deny","existingVMs":"AllowOnViolation"}}`, false),
		Entry("deny power-on", `{"governs":{"existingVMs":"DenyPowerOn"}}`, false),
		Entry("invalid enforcement", `{"governs":{"enforcement":"Allow"}}`, true),
		Entry("invalid existing-VM policy", `{"governs":{"existingVMs":"PowerOff"}}`, true),
	)

	It("preserves all zone states through CreateOrUpdate and storage", func() {
		class := ctx.vmClass.DeepCopy()
		Expect(ctx.Client.Create(ctx, class)).To(Succeed())
		key := ctrlclient.ObjectKeyFromObject(class)
		for _, zones := range []*[]string{nil, ptr.To([]string{}), ptr.To([]string{"a", "b"}), nil} {
			_, err := ctrlutil.CreateOrUpdate(ctx, ctx.Client, class, func() error {
				class.Spec.Zones = zones
				class.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{Zones: zones}
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			stored := &vmopv1.VirtualMachineClass{}
			Expect(ctx.Client.Get(ctx, key, stored)).To(Succeed())
			Expect(stored.Spec.Zones).To(Equal(zones))
			Expect(stored.Spec.Governs).NotTo(BeNil())
			Expect(stored.Spec.Governs.Zones).To(Equal(zones))
			// The schema foundation must not install webhook defaults early.
			Expect(stored.Spec.Governs.Enforcement).To(BeEmpty())
		}
	})

	fieldPath := field.NewPath("spec", "resources", "requests", "memory")
	invalidMemField := field.Invalid(fieldPath, "2Gi", "memory request must not be larger than the memory limit")
	DescribeTable("create table", validateCreate,
		Entry("should work", true, "", nil),
		Entry("should not work for invalid", false, invalidMemField.Error(), nil),
	)

	DescribeTable("class policy schema", validateSchema,
		Entry("accepts numeric and string quantities", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{
				Min: ptr.To(resource.MustParse("1")), Max: ptr.To(resource.MustParse("4")), Default: ptr.To(resource.MustParse("2")),
			}
			class.Spec.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{
				Min: ptr.To(resource.MustParse("1Gi")), Max: ptr.To(resource.MustParse("8Gi")), Default: ptr.To(resource.MustParse("4Gi")),
			}
		}, false),
		Entry("rejects bound without default", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1"))}
		}, true),
		Entry("rejects default below minimum", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{
				Min: ptr.To(resource.MustParse("4")), Default: ptr.To(resource.MustParse("2")),
			}
		}, true),
		Entry("rejects default above maximum", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{
				Max: ptr.To(resource.MustParse("4")), Default: ptr.To(resource.MustParse("8")),
			}
		}, true),
		Entry("rejects fractional CPU size defaults", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1500m"))}
		}, true),
		Entry("rejects CPU size defaults above int32", func(class *vmopv1.VirtualMachineClass) {
			class.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("2147483648"))}
		}, true),
	)

}

func intgTestsValidateDelete() {
	var (
		err error
		ctx *intgValidatingWebhookContext
	)

	BeforeEach(func() {
		ctx = newIntgValidatingWebhookContext()
		err = ctx.Client.Create(ctx, ctx.vmClass)
		Expect(err).ToNot(HaveOccurred())
	})
	JustBeforeEach(func() {
		err = ctx.Client.Delete(suite, ctx.vmClass)
	})
	AfterEach(func() {
		err = nil
		ctx = nil
	})

	When("delete is performed", func() {
		It("should allow the request", func() {
			Expect(ctx.Namespace).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
	})
}
