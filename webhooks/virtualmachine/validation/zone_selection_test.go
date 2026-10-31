// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/util/ptr"
)

func TestZoneAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, value, zone, placed                      string
		update, operator, metadata, disabled, rejected bool
	}{
		{name: "ambiguous omission", rejected: true},
		{name: "two", value: "2"},
		{name: "six", value: "6"},
		{name: "ten", value: "10", rejected: true},
		{name: "explicit wrong zone", value: "6", zone: "a", rejected: true},
		{name: "explicit resolves omission", zone: "a"},
		{name: "placed VM only checks assigned zone", value: "6", placed: "a", update: true, rejected: true},
		{name: "placed VM cannot move", value: "6", zone: "b", placed: "a", update: true, rejected: true},
		{name: "metadata housekeeping", value: "10", update: true, metadata: true},
		{name: "operator refresh", value: "10", operator: true},
		{name: "disabled capability", value: "10", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := &pkgctx.WebhookRequestContext{WebhookContext: &pkgctx.WebhookContext{Context: pkgcfg.NewContextWithDefaultConfig()}, IsVMOperatorAccount: tc.operator}
			pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = !tc.disabled })
			scheme := runtime.NewScheme()
			g.Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
			g.Expect(topologyv1.AddToScheme(scheme)).To(Succeed())
			var objects []ctrlclient.Object
			for i, name := range []string{"a", "b"} {
				objects = append(objects, &topologyv1.Zone{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tenant"}})
				minimum, maximum := "2", "4"
				if i == 1 {
					minimum, maximum = "5", "8"
				}
				objects = append(objects, &vmopv1.VirtualMachineClass{ObjectMeta: metav1.ObjectMeta{Name: "governor-" + name, Namespace: "tenant"}, Spec: vmopv1.VirtualMachineClassSpec{Governs: &vmopv1.VirtualMachineClassGovernance{Zones: &[]string{name}}, Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Default: ptr.To(resource.MustParse(minimum)), Min: ptr.To(resource.MustParse(minimum)), Max: ptr.To(resource.MustParse(maximum))}}}}})
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			v := validator{zoneReader: reader}
			vm := &vmopv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Labels: map[string]string{corev1.LabelTopologyZone: tc.zone}}}
			if tc.value != "" {
				vm.Spec.Resources = &vmopv1.VirtualMachineResourcesSpec{Size: &vmopv1.VirtualMachineResourceQuantity{CPU: ptr.To(resource.MustParse(tc.value))}}
			}
			var old *vmopv1.VirtualMachine
			if tc.update {
				old = vm.DeepCopy()
				old.Status.Zone = tc.placed
				if !tc.metadata {
					old.Spec.Resources = nil
					old.Labels = nil
				}
			}
			errs := v.validateZoneSelection(ctx, vm, old)
			if tc.rejected {
				g.Expect(errs).NotTo(BeEmpty())
			} else {
				g.Expect(errs).To(BeEmpty(), fmt.Sprint(errs))
			}
			if tc.rejected {
				g.Expect(errs[0].Field).To(ContainSubstring(corev1.LabelTopologyZone))
			}
		})
	}
}

type zoneErrorReader struct{ ctrlclient.Reader }

func (zoneErrorReader) List(context.Context, ctrlclient.ObjectList, ...ctrlclient.ListOption) error {
	return fmt.Errorf("discovery unavailable")
}
func TestZoneAdmissionReadFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := &pkgctx.WebhookRequestContext{WebhookContext: &pkgctx.WebhookContext{Context: pkgcfg.NewContextWithDefaultConfig()}}
	pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = true })
	v := validator{zoneReader: zoneErrorReader{}}
	g.Expect(v.validateZoneSelection(ctx, &vmopv1.VirtualMachine{}, nil)).To(HaveLen(1))
	pkgcfg.SetContext(ctx, func(c *pkgcfg.Config) { c.Features.VMServiceClasslessVM = false })
	g.Expect(v.validateZoneSelection(ctx, &vmopv1.VirtualMachine{}, nil)).To(BeEmpty())
}
