// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachineclass_test

import (
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/controllers/virtualmachineclass"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	"github.com/vmware-tanzu/vm-operator/pkg/constants/testlabels"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

var _ = Describe("Class governance reconciliation", Label(testlabels.Controller), func() {
	It("reports drift, stable transitions, namespace isolation, deletion and live capability changes", func() {
		ctx := pkgcfg.NewContextWithDefaultConfig()
		scheme := runtime.NewScheme()
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		Expect(topologyv1.AddToScheme(scheme)).To(Succeed())
		ceiling := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "ceiling", Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1")), Max: ptr.To(resource.MustParse("8")), Default: ptr.To(resource.MustParse("2"))}}}}}
		class := ceiling.DeepCopy()
		class.Name = "selectable"
		class.Spec.Governs = nil
		class.Generation = 2
		class.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("4"))
		foreign := ceiling.DeepCopy()
		foreign.Namespace = "other"
		zone := &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: "zone-a", Namespace: "tenant"}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&vmopv1.VirtualMachineClass{}).WithObjects(class, ceiling, foreign, zone).Build()
		r := virtualmachineclass.NewReconciler(ctx, c, logr.Discard(), nil)
		request := ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(class)}
		result, err := r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(class.Status.Zones).To(BeEmpty())
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = true })
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(class.Status.ObservedGeneration).To(Equal(int64(2)))
		Expect(class.Status.Zones).To(HaveLen(1))
		condition := apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance)
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		before := class.Status.DeepCopy()
		rv := class.ResourceVersion
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(class.Status).To(Equal(*before))
		Expect(class.ResourceVersion).To(Equal(rv))
		ceiling.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("2"))
		Expect(c.Update(ctx, ceiling)).To(Succeed())
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance).Status).To(Equal(metav1.ConditionFalse))
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = false })
		Expect(c.Delete(ctx, ceiling)).To(Succeed())
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance).Status).To(Equal(metav1.ConditionFalse))
		pkgcfg.SetContext(ctx, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = true })
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance).Reason).To(Equal(vmopv1.VirtualMachineClassNoGoverningClassReason))
		class.Spec.Zones = &[]string{}
		Expect(c.Update(ctx, class)).To(Succeed())
		_, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, request.NamespacedName, class)).To(Succeed())
		Expect(class.Status.Zones).To(BeEmpty())
	})
})

var _ = Describe("Class governance watches", Label(testlabels.Controller, testlabels.EnvTest, testlabels.API), func() {
	It("rechecks selectable classes when a governing class is narrowed", func() {
		ctx := suite.NewIntegrationTestContext()
		DeferCleanup(ctx.AfterEach)
		pkgcfg.SetContext(suite, func(config *pkgcfg.Config) {
			config.Features.VMServiceClasslessVM = true
			config.Features.ImmutableClasses = false
		})
		DeferCleanup(func() {
			pkgcfg.SetContext(suite, func(config *pkgcfg.Config) { config.Features.VMServiceClasslessVM = false })
		})
		ceiling := &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "governor-", Namespace: ctx.Namespace}, Spec: vmopv1.VirtualMachineClassSpec{Zones: &[]string{"governance-zone"}, Governs: &vmopv1.VirtualMachineClassGovernance{ExistingVMs: "AllowOnViolation"}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Min: ptr.To(resource.MustParse("1")), Max: ptr.To(resource.MustParse("8")), Default: ptr.To(resource.MustParse("2"))}}}}}
		class := ceiling.DeepCopy()
		class.GenerateName = "selectable-"
		class.Spec.Governs = nil
		class.Spec.Resources.Size.CPU = vmopv1.FixedClassResource(resource.MustParse("4"))
		Expect(ctx.Client.Create(ctx, ceiling)).To(Succeed())
		DeferCleanup(func() { Expect(ctx.Client.Delete(ctx, ceiling)).To(Succeed()) })
		Expect(ctx.Client.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(ctx.Client.Delete(ctx, class)).To(Succeed()) })
		Eventually(func(g Gomega) {
			g.Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			g.Expect(class.Status.Zones).To(HaveLen(1))
			g.Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance).Status).To(Equal(metav1.ConditionTrue))
		}).Should(Succeed())
		Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(ceiling), ceiling)).To(Succeed())
		ceiling.Spec.Resources.Size.CPU.Max = ptr.To(resource.MustParse("2"))
		Expect(ctx.Client.Update(ctx, ceiling)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(ctx.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(class), class)).To(Succeed())
			g.Expect(apimeta.FindStatusCondition(class.Status.Zones[0].Conditions, vmopv1.VirtualMachineClassConditionWithinGovernance).Status).To(Equal(metav1.ConditionFalse))
		}).Should(Succeed())
	})
})
