// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

var _ = Describe("Device memory admission", func() {
	for _, update := range []bool{false, true} {
		DescribeTable("requires full memory on create and spec updates", func(direct, locked, equal, operator, admin, enabled bool) {
			ctx := newUnitTestContextForValidatingWebhook(update)
			pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = enabled })
			ctx.IsVMOperatorAccount = operator
			ctx.IsPrivilegedAccount = admin || operator
			if ctx.oldVM != nil {
				bypassUpgradeCheck(&ctx.Context, ctx.vm, ctx.oldVM)
			}
			doTestWithContext(ctx, testParams{expectAllowed: !enabled || locked || equal || operator, setup: func(ctx *unitValidatingWebhookContext) {
				if ctx.vm.Spec.Hardware == nil {
					ctx.vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{}
				}
				ctx.vm.Spec.Hardware.Devices = &vmopv1.VirtualMachineDevicesSpec{}
				if direct {
					ctx.vm.Spec.Hardware.Devices.DynamicDirectPathIODevices = []vmopv1.VirtualMachineDynamicDirectPathIODevice{{VendorID: 1, DeviceID: 2, Count: 1}}
				} else {
					ctx.vm.Spec.Hardware.Devices.VGPUDevices = []vmopv1.VirtualMachineVGPUDevice{{ProfileName: "gpu", Count: 1}}
				}
				if locked {
					ctx.vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(true)}
				}
				if equal {
					ctx.vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("2Gi"))}, Requests: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("2048Mi"))}}
				}
			}})
		},
			Entry("rejects vGPU", false, false, false, false, false, true),
			Entry("rejects DirectPath", true, false, false, false, false, true),
			Entry("accepts lock", false, true, false, false, false, true),
			Entry("accepts equal reservation", true, false, true, false, false, true),
			Entry("exempts operator", false, false, false, true, false, true),
			Entry("does not exempt other privileged users", false, false, false, false, true, true),
			Entry("capability off", false, false, false, false, false, false),
		)
	}
	It("permits metadata, finalizer and status-only updates of under-reserved VMs", func() {
		ctx := newUnitTestContextForValidatingWebhook(true)
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		ctx.IsVMOperatorAccount = false
		if ctx.oldVM != nil {
			bypassUpgradeCheck(&ctx.Context, ctx.vm, ctx.oldVM)
		}
		ctx.vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: &vmopv1.VirtualMachineDevicesSpec{VGPUDevices: []vmopv1.VirtualMachineVGPUDevice{{ProfileName: "gpu", Count: 1}}}}
		ctx.oldVM.Spec = *ctx.vm.Spec.DeepCopy()
		doTestWithContext(ctx, testParams{expectAllowed: true, setup: func(ctx *unitValidatingWebhookContext) {
			ctx.vm.Labels = map[string]string{"test": "metadata"}
			ctx.vm.Finalizers = append(ctx.vm.Finalizers, "test.example/finalizer")
			ctx.vm.Status.Hardware = &vmopv1.VirtualMachineHardwareStatus{VGPUs: []vmopv1.VirtualMachineHardwareVGPUStatus{{}}}
		}})
	})
	It("names both supported fixes in the rejection", func() {
		ctx := newUnitTestContextForValidatingWebhook(false)
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		ctx.IsVMOperatorAccount = false
		doTestWithContext(ctx, testParams{expectAllowed: false, setup: func(ctx *unitValidatingWebhookContext) {
			ctx.vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{Devices: &vmopv1.VirtualMachineDevicesSpec{VGPUDevices: []vmopv1.VirtualMachineVGPUDevice{{ProfileName: "gpu", Count: 1}}}}
		}, validate: doValidateWithMsg("vGPU and DirectPath I/O devices require full guest memory reservation: set spec.memoryAdvanced.reservationLockedToMax=true, or set spec.resources.requests.memory equal to spec.resources.size.memory")})
	})
	It("does not let a status-only exemption bypass a simultaneous spec change", func() {
		ctx := newUnitTestContextForValidatingWebhook(true)
		pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		ctx.IsVMOperatorAccount = false
		if ctx.oldVM != nil {
			bypassUpgradeCheck(&ctx.Context, ctx.vm, ctx.oldVM)
		}
		ctx.oldVM.Status.Hardware = &vmopv1.VirtualMachineHardwareStatus{VGPUs: []vmopv1.VirtualMachineHardwareVGPUStatus{{}}}
		doTestWithContext(ctx, testParams{expectAllowed: false, setup: func(ctx *unitValidatingWebhookContext) {
			ctx.vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{Memory: ptr.To(resource.MustParse("4Gi"))}}
		}})
	})
})

var _ = Describe("Device memory API-server admission", Label("envtest"), func() {
	It("rejects insufficient reservation through the installed webhook", func() {
		before := pkgcfg.FromContext(suite).Features.VMServiceClasslessVM
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		DeferCleanup(func() { pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = before }) })
		ctx := newIntgValidatingWebhookContext()
		DeferCleanup(ctx.AfterEach)
		if ctx.vm.Spec.Hardware == nil {
			ctx.vm.Spec.Hardware = &vmopv1.VirtualMachineHardwareSpec{}
		}
		ctx.vm.Spec.Hardware.Devices = &vmopv1.VirtualMachineDevicesSpec{VGPUDevices: []vmopv1.VirtualMachineVGPUDevice{{ProfileName: "gpu", Count: 1}}}
		err := ctx.Client.Create(ctx, ctx.vm)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("devices require full guest memory reservation"))
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
		Expect(ctx.Client.Create(ctx, ctx.vm.DeepCopy(), ctrlclient.DryRunAll)).To(Succeed())
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		err = ctx.Client.Create(ctx, ctx.vm.DeepCopy(), ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("devices require full guest memory reservation"))
		ctx.vm.Spec.MemoryAdvanced = &vmopv1.VirtualMachineMemoryAdvancedSpec{ReservationLockedToMax: ptr.To(true)}
		Expect(ctx.Client.Create(ctx, ctx.vm)).To(Succeed())
	})
})
