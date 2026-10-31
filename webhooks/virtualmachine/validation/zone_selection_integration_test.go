// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

var _ = Describe("Value-based zone admission through API server", Label("envtest"), func() {
	It("requires a zone only while supplied CPU values leave different defaults unresolved", func() {
		previous := pkgcfg.FromContext(suite).Features.VMServiceClasslessVM
		pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
		DeferCleanup(func() {
			pkgcfg.SetContext(suite, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = previous })
		})
		ctx := newIntgValidatingWebhookContext()
		DeferCleanup(ctx.AfterEach)
		for i, name := range []string{"a", "b", "c"} {
			zone := &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ctx.Namespace}, Spec: topologyv1.ZoneSpec{Zone: topologyv1.AvailabilityZoneReference{APIVersion: topologyv1.GroupVersion.String(), Name: name}}}
			Expect(ctx.Client.Create(ctx, zone)).To(Succeed())
			if i == 2 {
				continue
			}
			zones := []string{"a"}
			minimum, maximum := "2", "4"
			if i == 1 {
				zones = []string{"b", "c"}
				minimum, maximum = "5", "8"
			}
			governor := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "governor-" + name, Namespace: ctx.Namespace}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &zones}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse(minimum)), Max: ptr.To(resource.MustParse(maximum)), Default: ptr.To(resource.MustParse(minimum))}}}}}
			Expect(ctx.Client.Create(ctx, governor)).To(Succeed())
		}
		ctx.vm.Spec.Resources = nil
		err := ctx.Client.Create(ctx, ctx.vm.DeepCopy(), ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("zone-dependent fields resources.size.cpu"))
		for _, value := range []string{"2", "6"} {
			ctx.vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse(value))}}
			Expect(ctx.Client.Create(ctx, ctx.vm.DeepCopy(), ctrlclient.DryRunAll)).To(Succeed())
		}
		ctx.vm.Spec.Resources.Size.CPU = ptr.To(resource.MustParse("10"))
		err = ctx.Client.Create(ctx, ctx.vm.DeepCopy(), ctrlclient.DryRunAll)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no eligible zones"))
	})
})
