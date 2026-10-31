// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	jsonpatch "github.com/evanphx/json-patch/v5"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	vmopv1common "github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/webhooks/virtualmachineclass/mutation"
	"github.com/vmware-tanzu/vm-operator/webhooks/virtualmachineclass/validation"

	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/builder"
)

func unitTests() {
	Describe(
		"Create",
		Label(
			testlabels.Create,
			testlabels.API,
			testlabels.Validation,
			testlabels.Webhook,
		),
		unitTestsValidateCreate,
	)
	Describe(
		"Update",
		Label(
			testlabels.Update,
			testlabels.API,
			testlabels.Validation,
			testlabels.Webhook,
		),
		unitTestsValidateUpdate,
	)
	Describe(
		"Delete",
		Label(
			testlabels.Delete,
			testlabels.API,
			testlabels.Validation,
			testlabels.Webhook,
		),
		unitTestsValidateDelete,
	)
}

type unitValidatingWebhookContext struct {
	builder.UnitTestContextForValidatingWebhook
	vmClass    *vmopv1.VirtualMachineClass
	oldVMClass *vmopv1.VirtualMachineClass
}

func newUnitTestContextForValidatingWebhook(isUpdate bool) *unitValidatingWebhookContext {
	vmClass := builder.DummyVirtualMachineClassGenName()
	obj, err := builder.ToUnstructured(vmClass)
	Expect(err).ToNot(HaveOccurred())

	var oldVMClass *vmopv1.VirtualMachineClass
	var oldObj *unstructured.Unstructured

	if isUpdate {
		oldVMClass = vmClass.DeepCopy()
		oldObj, err = builder.ToUnstructured(oldVMClass)
		Expect(err).ToNot(HaveOccurred())
	}

	return &unitValidatingWebhookContext{
		UnitTestContextForValidatingWebhook: *suite.NewUnitTestContextForValidatingWebhook(obj, oldObj),
		vmClass:                             vmClass,
		oldVMClass:                          oldVMClass,
	}
}

func unitTestsValidateCreate() {
	var (
		ctx *unitValidatingWebhookContext
	)

	type createArgs struct {
		invalidCPURequest    bool
		invalidMemoryRequest bool
		noCPULimit           bool
		noMemoryLimit        bool
	}

	validateCreate := func(args createArgs, expectedAllowed bool, expectedReason string, expectedErr error) {
		var err error

		if args.invalidCPURequest {
			ctx.vmClass.Spec.Resources.Requests.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("2Gi"))}
			ctx.vmClass.Spec.Resources.Limits.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1Gi"))}
		}
		if args.invalidMemoryRequest {
			ctx.vmClass.Spec.Resources.Requests.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("2Gi"))}
			ctx.vmClass.Spec.Resources.Limits.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("1Gi"))}
		}
		if args.noCPULimit {
			ctx.vmClass.Spec.Resources.Limits.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("0"))}
		}
		if args.noMemoryLimit {
			ctx.vmClass.Spec.Resources.Limits.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("0"))}
		}

		ctx.WebhookRequestContext.Obj, err = builder.ToUnstructured(ctx.vmClass)
		Expect(err).ToNot(HaveOccurred())

		response := ctx.ValidateCreate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(Equal(expectedAllowed))
		if expectedReason != "" {
			Expect(string(response.Result.Reason)).To(Equal(expectedReason))
		}
		if expectedErr != nil {
			Expect(response.Result.Message).To(Equal(expectedErr.Error()))
		}
	}

	BeforeEach(func() {
		ctx = newUnitTestContextForValidatingWebhook(false)
	})
	AfterEach(func() {
		ctx = nil
	})

	reqPath := field.NewPath("spec", "resources", "requests")
	invalidCPUField := field.Invalid(reqPath.Child("cpu"), "2Gi", "CPU request must not be larger than the CPU limit")
	invalidMemField := field.Invalid(reqPath.Child("memory"), "2Gi", "memory request must not be larger than the memory limit")
	DescribeTable("create table", validateCreate,
		Entry("should allow valid", createArgs{}, true, nil, nil),
		Entry("should allow no cpu limit", createArgs{noCPULimit: true}, true, nil, nil),
		Entry("should allow no memory limit", createArgs{noMemoryLimit: true}, true, nil, nil),
		Entry("should deny invalid cpu request", createArgs{invalidCPURequest: true}, false, invalidCPUField.Error(), nil),
		Entry("should deny invalid memory request", createArgs{invalidMemoryRequest: true}, false, invalidMemField.Error(), nil),
	)
}

func unitTestsValidateUpdate() {
	var (
		ctx      *unitValidatingWebhookContext
		response admission.Response
	)

	type updateArgs struct {
		changeHwCPU    bool
		changeHwMemory bool
		changeCPU      bool
		changeMemory   bool
	}

	validateUpdate := func(args updateArgs, expectedAllowed bool, expectedReason string, expectedErr error) {
		var err error

		if args.changeHwCPU {
			ctx.vmClass.Spec.Resources.Size.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("64"))}
		}
		if args.changeHwMemory {
			ctx.vmClass.Spec.Resources.Size.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("5Gi"))}
		}
		if args.changeCPU {
			ctx.vmClass.Spec.Resources.Requests.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("5Gi"))}
			ctx.vmClass.Spec.Resources.Limits.CPU = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("10Gi"))}
		}
		if args.changeMemory {
			ctx.vmClass.Spec.Resources.Requests.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("5Gi"))}
			ctx.vmClass.Spec.Resources.Limits.Memory = &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse("10Gi"))}
		}

		ctx.WebhookRequestContext.Obj, err = builder.ToUnstructured(ctx.vmClass)
		Expect(err).ToNot(HaveOccurred())

		response := ctx.ValidateUpdate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(Equal(expectedAllowed))
		if expectedReason != "" {
			// Construct the complete reason here as ctx.vmClass.Spec is nil when initializing the DescribeTable below.
			completeReason := field.Invalid(field.NewPath("spec"), ctx.vmClass.Spec, expectedReason).Error()
			Expect(string(response.Result.Reason)).To(ContainSubstring(completeReason))
		}
		if expectedErr != nil {
			Expect(response.Result.Message).To(ContainSubstring(expectedErr.Error()))
		}
	}

	BeforeEach(func() {
		ctx = newUnitTestContextForValidatingWebhook(true)
	})
	AfterEach(func() {
		ctx = nil
	})

	DescribeTable("update VM Class spec", validateUpdate,
		Entry("should allow", updateArgs{}, true, nil, nil),
		Entry("should allow hw cpu change", updateArgs{changeHwCPU: true}, true, nil, nil),
		Entry("should allow hw memory change", updateArgs{changeHwMemory: true}, true, nil, nil),
		Entry("should allow policy cpu change", updateArgs{changeCPU: true}, true, nil, nil),
		Entry("should allow policy memory change", updateArgs{changeMemory: true}, true, nil, nil),
	)

	DescribeTable("update table", validateUpdate,
		Entry("should allow", updateArgs{}, true, nil, nil),
		Entry("should deny hw cpu change", updateArgs{changeHwCPU: true}, true, nil, nil),
		Entry("should deny hw memory change", updateArgs{changeHwMemory: true}, true, nil, nil),
		Entry("should deny policy cpu change", updateArgs{changeCPU: true}, true, nil, nil),
		Entry("should deny policy memory change", updateArgs{changeMemory: true}, true, nil, nil),
	)

	When("the update is performed while object deletion", func() {
		JustBeforeEach(func() {
			t := metav1.Now()
			ctx.WebhookRequestContext.Obj.SetDeletionTimestamp(&t)
			response = ctx.ValidateUpdate(&ctx.WebhookRequestContext)
		})

		It("should allow the request", func() {
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Result).ToNot(BeNil())
		})
	})
}

func unitTestsValidateDelete() {
	var (
		ctx      *unitValidatingWebhookContext
		response admission.Response
	)

	BeforeEach(func() {
		ctx = newUnitTestContextForValidatingWebhook(false)
	})
	AfterEach(func() {
		ctx = nil
	})

	When("the delete is performed", func() {
		JustBeforeEach(func() {
			response = ctx.ValidateDelete(&ctx.WebhookRequestContext)
		})

		It("should allow the request", func() {
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Result).ToNot(BeNil())
		})
	})
}

var _ = Describe("Governance admission", Label(testlabels.API, testlabels.Validation, testlabels.Webhook), func() {
	for _, update := range []bool{false, true} {
		Context(fmt.Sprintf("update=%t", update), func() {
			DescribeTable("governing scopes", func(availability, governing, existingAvailability, existingGoverning *[]string, allowed bool) {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Zones, candidate.Spec.Governs.Zones = availability, governing
				ceiling := governanceTestClass("ceiling", "tenant")
				ceiling.Spec.Zones, ceiling.Spec.Governs.Zones = existingAvailability, existingGoverning
				response := governanceTestResponse(governanceTestContext(candidate, update, ceiling), update)
				Expect(response.Allowed).To(Equal(allowed), "%v", response.Result)
				if !allowed {
					Expect(string(response.Result.Reason)).To(ContainSubstring("overlaps governing class"))
				}
			},
				Entry("universal scopes overlap without current Zones", nil, nil, nil, nil, false),
				Entry("explicit future zone overlaps wildcard", &[]string{"future"}, nil, nil, nil, false),
				Entry("wildcard overlaps explicit future zone", nil, nil, &[]string{"future"}, nil, false),
				Entry("governance inherits availability", &[]string{"a"}, nil, &[]string{"a"}, nil, false),
				Entry("explicit governance narrows availability", &[]string{"a", "b"}, &[]string{"a"}, &[]string{"b"}, nil, true),
				Entry("explicit governance intersects", nil, &[]string{"a"}, nil, &[]string{"a"}, false),
				Entry("empty governing scope never overlaps", nil, &[]string{}, nil, nil, true),
				Entry("empty availability never overlaps", &[]string{}, nil, nil, nil, true),
				Entry("empty existing governance never overlaps", nil, nil, nil, &[]string{}, true),
				Entry("disjoint availability", &[]string{"a"}, nil, &[]string{"b"}, nil, true),
			)
			It("requires existingVMs even for empty governance", func() {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Governs.ExistingVMs = ""
				candidate.Spec.Governs.Zones = &[]string{}
				response := governanceTestResponse(governanceTestContext(candidate, update), update)
				Expect(response.Allowed).To(BeFalse())
				Expect(string(response.Result.Reason)).To(ContainSubstring("spec.governs.existingVMs"))
			})
			It("checks selectable ranges against the governing ceiling", func() {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Governs = nil
				candidate.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("16"))
				response := governanceTestResponse(governanceTestContext(candidate, update, governanceTestClass("ceiling", "tenant")), update)
				Expect(response.Allowed).To(BeFalse())
				Expect(string(response.Result.Reason)).To(ContainSubstring("spec.resources.size.cpu"))
				Expect(string(response.Result.Reason)).To(ContainSubstring(`governing class "ceiling"`))
			})
			It("checks selectable scope outside the class's own governing scope", func() {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Governs.Zones = &[]string{"a"}
				candidate.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("16"))
				ceiling := governanceTestClass("ceiling", "tenant")
				ceiling.Spec.Governs.Zones = &[]string{"b"}
				response := governanceTestResponse(governanceTestContext(candidate, update, ceiling), update)
				Expect(response.Allowed).To(BeFalse())
				Expect(string(response.Result.Reason)).To(ContainSubstring("zones [b]"))
			})
			DescribeTable("checks typed ExtraConfig defaults", func(key string, allowed bool) {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Governs = nil
				candidate.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{ExtraConfig: &vmopv1.ExtraConfigListPolicy{
					Default: &[]vmopv1common.KeyValuePair{{Key: key, Value: "value"}},
				}}
				ceiling := governanceTestClass("ceiling", "tenant")
				ceiling.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{ExtraConfig: &vmopv1.ExtraConfigListPolicy{
					Allowed: &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Glob: ptr.To("guestinfo.*")}}},
					Denied:  &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Fixed: ptr.To("guestinfo.secret")}}},
				}}
				response := governanceTestResponse(governanceTestContext(candidate, update, ceiling), update)
				Expect(response.Allowed).To(Equal(allowed), "%v", response.Result)
				if !allowed {
					Expect(string(response.Result.Reason)).To(ContainSubstring("spec.advanced.extraConfig"))
				}
			},
				Entry("allowed key", "guestinfo.visible", true),
				Entry("deny wins over allow", "guestinfo.secret", false),
				Entry("outside allow list", "other.key", false),
			)

			It("ignores deleting and foreign-namespace classes", func() {
				candidate := governanceTestClass("candidate", "tenant")
				foreign := governanceTestClass("foreign", "vmware-system-vmop")
				deleting := governanceTestClass("deleting", "tenant")
				deleting.DeletionTimestamp, deleting.Finalizers = ptr.To(metav1.Now()), []string{"test"}
				Expect(governanceTestResponse(governanceTestContext(candidate, update, foreign, deleting), update).Allowed).To(BeTrue())
			})
			It("enforces the same rules in the provider system namespace", func() {
				ctx := governanceTestContext(governanceTestClass("candidate", "vmware-system-vmop"), update, governanceTestClass("ceiling", "vmware-system-vmop"))
				Expect(governanceTestResponse(ctx, update).Allowed).To(BeFalse())
			})
			It("observes capability changes without rebuilding the validator", func() {
				candidate := governanceTestClass("candidate", "tenant")
				candidate.Spec.Governs.ExistingVMs = ""
				ctx := governanceTestContext(candidate, update, governanceTestClass("ceiling", "tenant"))
				for _, enabled := range []bool{false, true, false} {
					pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = enabled })
					Expect(governanceTestResponse(ctx, update).Allowed).To(Equal(!enabled))
				}
			})
		})
	}
	DescribeTable("pre-existing governing conflicts", func(candidateZones, leftZones, rightZones *[]string, allowed bool) {
		candidate := governanceTestClass("selectable", "tenant")
		candidate.Spec.Governs = nil
		candidate.Spec.Zones = candidateZones
		left, right := governanceTestClass("left", "tenant"), governanceTestClass("right", "tenant")
		left.Spec.Zones, right.Spec.Zones = leftZones, rightZones
		ctx := governanceTestContext(candidate, false, left, right)
		response := governanceTestResponse(ctx, false)
		Expect(response.Allowed).To(Equal(allowed), "%v", response.Result)
		if !allowed {
			Expect(string(response.Result.Reason)).To(ContainSubstring("ambiguous governance"))
		}
	},
		Entry("two universal ceilings reject a fitting class", nil, nil, nil, false),
		Entry("two explicit overlapping ceilings reject", &[]string{"a"}, &[]string{"a"}, &[]string{"a", "b"}, false),
		Entry("disjoint ceilings permit a class available in both", nil, &[]string{"a"}, &[]string{"b"}, true),
		Entry("conflict outside candidate scope is irrelevant", &[]string{"a"}, &[]string{"b"}, &[]string{"b"}, true),
		Entry("empty candidate availability is irrelevant", &[]string{}, nil, nil, true),
	)

	It("allows governor narrowing without checking already admitted classes", func() {
		candidate := governanceTestClass("ceiling", "tenant")
		candidate.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("2"))
		selectable := governanceTestClass("selectable", "tenant")
		selectable.Spec.Governs = nil
		ctx := governanceTestContext(candidate, true, selectable, governanceTestClass("ceiling", "tenant"))
		Expect(governanceTestResponse(ctx, true).Allowed).To(BeTrue())
	})
	It("permits metadata updates outside a narrowed ceiling", func() {
		candidate := governanceTestClass("selectable", "tenant")
		candidate.Spec.Governs = nil
		candidate.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("16"))
		ctx := governanceTestContext(candidate, true, governanceTestClass("ceiling", "tenant"))
		ctx.OldObj = ctx.Obj.DeepCopy()
		ctx.Obj.SetLabels(map[string]string{"controller": "update"})
		Expect(governanceTestResponse(ctx, true).Allowed).To(BeTrue())
	})
	DescribeTable("class mutation and validation pipeline after capability activation", func(specEdit bool) {
		candidate := governanceTestClass("selectable", "tenant")
		candidate.Spec.Governs.Zones = &[]string{}
		candidate.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("16"))
		candidate.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{
			Default: ptr.To([]vmopv1.VGPUDevice{{ProfileName: "gpu"}}),
		}
		ctx := governanceTestContext(candidate, true, governanceTestClass("ceiling", "tenant"))
		ctx.Obj.SetAPIVersion(vmopv1.GroupVersion.String())
		ctx.Obj.SetKind("VirtualMachineClass")
		ctx.OldObj = ctx.Obj.DeepCopy()
		ctx.Obj.SetLabels(map[string]string{"controller": "housekeeping"})
		ctx.Op = admissionv1.Update
		if specEdit {
			Expect(unstructured.SetNestedField(ctx.Obj.Object, "spec edit", "spec", "description")).To(Succeed())
		}
		raw, err := json.Marshal(ctx.Obj)
		Expect(err).NotTo(HaveOccurred())
		ctx.RawObj = raw
		response := mutation.NewMutator(nil).Mutate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(BeTrue())
		if specEdit {
			Expect(response.Patches).NotTo(BeEmpty())
			patchJSON, err := json.Marshal(response.Patches)
			Expect(err).NotTo(HaveOccurred())
			patch, err := jsonpatch.DecodePatch(patchJSON)
			Expect(err).NotTo(HaveOccurred())
			mutated, err := patch.Apply(raw)
			Expect(err).NotTo(HaveOccurred())
			ctx.Obj = &unstructured.Unstructured{}
			Expect(json.Unmarshal(mutated, ctx.Obj)).To(Succeed())
		} else {
			Expect(response.Patches).To(BeEmpty(), "housekeeping must not acquire spec defaults")
		}
		validationResponse := ctx.ValidateUpdate(&ctx.WebhookRequestContext)
		Expect(validationResponse.Allowed).To(Equal(!specEdit))
		if specEdit {
			Expect(string(validationResponse.Result.Reason)).To(ContainSubstring("spec.resources.size.cpu"))
		}
	}, Entry("metadata-only update", false), Entry("actual spec edit", true))

	It("permits updates while a noncompliant class is deleting", func() {
		candidate := governanceTestClass("selectable", "tenant")
		candidate.Spec.Governs.ExistingVMs = ""
		candidate.DeletionTimestamp = ptr.To(metav1.Now())
		Expect(governanceTestResponse(governanceTestContext(candidate, true), true).Allowed).To(BeTrue())
	})
	It("fails closed on lookup failure and skips lookup while disabled", func() {
		ctx := governanceTestContext(governanceTestClass("candidate", "tenant"), false)
		calls := 0
		failing := builder.NewFakeClientWithInterceptors(interceptor.Funcs{
			List: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectList, ...ctrlclient.ListOption) error {
				calls++
				return fmt.Errorf("lookup failed")
			},
		})
		ctx.Validator = validation.NewValidator(failing)
		response := ctx.ValidateCreate(&ctx.WebhookRequestContext)
		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result.Code).To(Equal(int32(500)))
		Expect(response.Result.Message).To(ContainSubstring("list classes for governance validation"))
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		Expect(ctx.ValidateCreate(&ctx.WebhookRequestContext).Allowed).To(BeTrue())
		Expect(calls).To(Equal(1))
	})
})

func governanceTestClass(name, namespace string) *vmopv1.VirtualMachineClass {
	return &vmopv1.VirtualMachineClass{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: vmopv1.VirtualMachineClassSpec{
			Governs: &vmopv1.VirtualMachineClassGovernance{ExistingVMs: vmopv1.VirtualMachineClassExistingVMsAllowOnViolation},
			Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{
				CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1")), Max: ptr.To(resource.MustParse("8")), Default: ptr.To(resource.MustParse("2"))},
			}},
		},
	}
}

func governanceTestContext(class *vmopv1.VirtualMachineClass, update bool, existing ...ctrlclient.Object) *builder.UnitTestContextForValidatingWebhook {
	obj, err := builder.ToUnstructured(class)
	Expect(err).NotTo(HaveOccurred())
	var old *unstructured.Unstructured
	if update {
		old = obj.DeepCopy()
		Expect(unstructured.SetNestedField(old.Object, "previous description", "spec", "description")).To(Succeed())
	}
	ctx := suite.NewUnitTestContextForValidatingWebhook(obj, old, existing...)
	pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
	return ctx
}

func governanceTestResponse(ctx *builder.UnitTestContextForValidatingWebhook, update bool) admission.Response {
	if update {
		return ctx.ValidateUpdate(&ctx.WebhookRequestContext)
	}
	return ctx.ValidateCreate(&ctx.WebhookRequestContext)
}
