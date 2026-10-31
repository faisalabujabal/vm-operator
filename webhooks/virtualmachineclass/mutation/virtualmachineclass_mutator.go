// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/pkg/builder"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

const (
	webHookName = "default"
)

// +kubebuilder:webhook:matchPolicy=Exact,path=/default-mutate-vmoperator-vmware-com-v1alpha6-virtualmachineclass,mutating=true,failurePolicy=fail,groups=vmoperator.vmware.com,resources=virtualmachineclasses,verbs=create;update,versions=v1alpha6,name=default.mutating.virtualmachineclass.v1alpha6.vmoperator.vmware.com,sideEffects=None,admissionReviewVersions=v1;v1beta1
// -kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclass,verbs=get;list
// -kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclass/status,verbs=get

// AddToManager adds the webhook to the provided manager.
func AddToManager(ctx *pkgctx.ControllerManagerContext, mgr ctrlmgr.Manager) error {
	hook, err := builder.NewMutatingWebhook(ctx, mgr, webHookName, NewMutator(nil))
	if err != nil {
		return fmt.Errorf("failed to create mutation webhook: %w", err)
	}
	mgr.GetWebhookServer().Register(hook.Path, hook)

	return nil
}

// NewMutator returns the package's Mutator.
func NewMutator(_ client.Client) builder.Mutator {
	return mutator{
		converter: runtime.DefaultUnstructuredConverter,
	}
}

type mutator struct {
	converter runtime.UnstructuredConverter
}

func (m mutator) Mutate(ctx *pkgctx.WebhookRequestContext) admission.Response {
	modified, err := m.vmClassFromUnstructured(ctx.Obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	var wasMutated bool
	defaultClass := ctx.Op == admissionv1.Create

	switch ctx.Op {
	case admissionv1.Create:
		// No-op
	case admissionv1.Update:
		oldObj, err := m.vmClassFromUnstructured(ctx.OldObj)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		// Metadata and finalizer writes must remain possible after governance
		// narrows or the capability changes. Only default actual spec edits.
		defaultClass = !apiequality.Semantic.DeepEqual(modified.Spec, oldObj.Spec)
		if SetControllerName(modified, oldObj) {
			wasMutated = true
		}
	case admissionv1.Delete, admissionv1.Connect:
		// No mutation is performed for these operations.
	}

	if defaultClass &&
		pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM {
		changed, err := vmclass.ApplyClassDefaults(&modified.Spec)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		wasMutated = wasMutated || changed
	}

	if !wasMutated {
		return admission.Allowed("")
	}

	rawModified, err := json.Marshal(modified)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(ctx.RawObj, rawModified)
}

func (m mutator) For() schema.GroupVersionKind {
	return vmopv1.GroupVersion.WithKind(reflect.TypeOf(vmopv1.VirtualMachineClass{}).Name())
}

// vmClassFromUnstructured returns the VirtualMachineClass from the unstructured object.
func (m mutator) vmClassFromUnstructured(obj runtime.Unstructured) (*vmopv1.VirtualMachineClass, error) {
	vmClass := &vmopv1.VirtualMachineClass{}
	if err := m.converter.FromUnstructured(obj.UnstructuredContent(), vmClass); err != nil {
		return nil, err
	}
	return vmClass, nil
}

// SetControllerName preserves the original value of spec.controllerName if
// an attempt is made to set the field with a non-empty value to empty.
// Return true if the field was preserved, otherwise false.
func SetControllerName(newObj, oldObj *vmopv1.VirtualMachineClass) bool {
	if newObj.Spec.ControllerName == "" {
		if oldObj.Spec.ControllerName != "" {
			newObj.Spec.ControllerName = oldObj.Spec.ControllerName
			return true
		}
	}
	return false
}
