// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package mutation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
	"github.com/vmware-tanzu/vm-operator/test/builder"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func intgTests() {
	Describe(
		"Mutate",
		Label(
			testlabels.Create,
			testlabels.Update,
			testlabels.Delete,
			testlabels.EnvTest,
			testlabels.API,
			testlabels.Mutation,
			testlabels.Webhook,
		),
		intgTestsMutating,
	)
}

type intgMutatingWebhookContext struct {
	builder.IntegrationTestContext
	vmClass *vmopv1.VirtualMachineClass
}

func newIntgMutatingWebhookContext() *intgMutatingWebhookContext {
	ctx := &intgMutatingWebhookContext{
		IntegrationTestContext: *suite.NewIntegrationTestContext(),
	}

	ctx.vmClass = builder.DummyVirtualMachineClassGenName()
	ctx.vmClass.Namespace = ctx.Namespace

	return ctx
}

func intgTestsMutating() {
	var (
		ctx     *intgMutatingWebhookContext
		vmClass *vmopv1.VirtualMachineClass
	)

	BeforeEach(func() {
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		ctx = newIntgMutatingWebhookContext()
		vmClass = ctx.vmClass.DeepCopy()
	})
	AfterEach(func() {
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		ctx = nil
	})

	It("persists governance and device defaults on create and spec update", func() {
		vmClass.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{ExistingVMs: vmopv1.VirtualMachineClassExistingVMsAllowOnViolation}
		vmClass.Spec.Hardware.Devices.VGPUDevices = &vmopv1.VGPUDeviceListPolicy{Default: ptr.To([]vmopv1.VGPUDevice{{ProfileName: "gpu"}})}
		vmClass.Spec.ConfigSpec = nil
		Expect(ctx.Client.Create(ctx, vmClass)).To(Succeed())
		key := ctrlclient.ObjectKeyFromObject(vmClass)
		Expect(ctx.Client.Get(ctx, key, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(Equal(vmopv1.VirtualMachineClassEnforcementDeny))
		Expect(vmClass.Spec.MemoryAdvanced.ReservationLockedToMax).To(Equal(vmopv1.FixedClassBool(true)))
		vmClass.Spec.Governs.Enforcement = ""
		vmClass.Spec.MemoryAdvanced.ReservationLockedToMax = vmopv1.FixedClassBool(false)
		Expect(ctx.Client.Update(ctx, vmClass)).To(Succeed())
		Expect(ctx.Client.Get(ctx, key, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(Equal(vmopv1.VirtualMachineClassEnforcementDeny))
		Expect(vmClass.Spec.MemoryAdvanced.ReservationLockedToMax).To(Equal(vmopv1.FixedClassBool(true)))
	})
	It("observes capability toggles without mutating metadata-only updates", func() {
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		vmClass.Spec.Governs = &vmopv1.VirtualMachineClassGovernance{}
		Expect(ctx.Client.Create(ctx, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(BeEmpty())
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		vmClass.Labels = map[string]string{"housekeeping": "true"}
		Expect(ctx.Client.Update(ctx, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(BeEmpty())
		vmClass.Spec.Description = "spec edit"
		Expect(ctx.Client.Update(ctx, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(Equal(vmopv1.VirtualMachineClassEnforcementDeny))
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		vmClass.Spec.Governs.Enforcement = ""
		Expect(ctx.Client.Update(ctx, vmClass)).To(Succeed())
		Expect(vmClass.Spec.Governs.Enforcement).To(BeEmpty())
	})
	It("keeps older provider writes outside the new mutating webhook", func() {
		legacy := &vmopv1a5.VirtualMachineClass{}
		Expect(legacy.ConvertFrom(vmClass)).To(Succeed())
		legacy.Spec.Hardware.Devices.VGPUDevices = []vmopv1a5.VGPUDevice{{ProfileName: "gpu"}}
		legacy.Spec.ConfigSpec = nil
		Expect(ctx.Client.Create(ctx, legacy)).To(Succeed())
		stored := &vmopv1.VirtualMachineClass{}
		// This suite installs admission but not the conversion webhook. Read
		// the original API version, then use its real converter for inspection.
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(legacy), legacy)).To(Succeed())
		Expect(legacy.ConvertTo(stored)).To(Succeed())
		Expect(stored.Spec.MemoryAdvanced).To(BeNil())
	})

	Describe("mutate", func() {
		Context("placeholder", func() {
			BeforeEach(func() {
			})

			It("should work", func() {
				err := ctx.Client.Create(ctx, vmClass)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
}
