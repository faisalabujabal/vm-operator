// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package virtualmachineclass

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgconst "github.com/vmware-tanzu/vm-operator/pkg/constants"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	pkglog "github.com/vmware-tanzu/vm-operator/pkg/log"
	"github.com/vmware-tanzu/vm-operator/pkg/patch"
	"github.com/vmware-tanzu/vm-operator/pkg/record"
	"github.com/vmware-tanzu/vm-operator/pkg/vmclass"
)

// AddToManager adds this package's controller to the provided manager.
func AddToManager(ctx *pkgctx.ControllerManagerContext, mgr manager.Manager) error {
	var (
		controlledType     = &vmopv1.VirtualMachineClass{}
		controlledTypeName = reflect.TypeOf(controlledType).Elem().Name()

		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
	)

	r := NewReconciler(
		ctx,
		mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName(controlledTypeName),
		record.New(mgr.GetEventRecorder(controllerNameShort)),
	)

	b := ctrl.NewControllerManagedBy(mgr).
		For(controlledType).
		Watches(controlledType, handler.EnqueueRequestsFromMapFunc(r.classesForDependency), builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || !e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp())
			},
		})).
		Watches(&topologyv1.Zone{}, handler.EnqueueRequestsFromMapFunc(r.classesForDependency), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ctx.GetMaxConcurrentReconciles(controllerNameShort, 1),
			LogConstructor:          pkglog.ControllerLogConstructor(controllerNameShort, controlledType, mgr.GetScheme()),
		})
	// Discovery types/controllers are registered at manager startup only when
	// the shared capability is enabled. Do not start unavailable informers.
	if pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM {
		b = b.Watches(&vimv1.ConfigTarget{}, handler.EnqueueRequestsFromMapFunc(r.classesForHardware)).
			Watches(&vimv1.VirtualMachineConfigOptions{}, handler.EnqueueRequestsFromMapFunc(r.classesForHardware)).
			Watches(&vimv1.VirtualMachineGuestOptions{}, handler.EnqueueRequestsFromMapFunc(r.classesForHardware))
	}
	return b.Complete(r)
}

func NewReconciler(
	ctx context.Context,
	client client.Client,
	logger logr.Logger,
	recorder record.Recorder) *Reconciler {
	return &Reconciler{
		Context:  ctx,
		Client:   client,
		Logger:   logger,
		Recorder: recorder,
	}
}

// Reconciler reconciles a VirtualMachineClass object.
type Reconciler struct {
	client.Client
	Context  context.Context
	Logger   logr.Logger
	Recorder record.Recorder
}

// +kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclassinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vmoperator.vmware.com,resources=virtualmachineclassinstances/status,verbs=get;update;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx = pkgcfg.JoinContext(ctx, r.Context)

	vmClass := &vmopv1.VirtualMachineClass{}
	if err := r.Get(ctx, req.NamespacedName, vmClass); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	vmClassCtx := &pkgctx.VirtualMachineClassContext{
		Context: ctx,
		Logger:  pkglog.FromContextOrDefault(ctx),
		VMClass: vmClass,
	}

	patchHelper, err := patch.NewHelper(vmClass, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to init patch helper for %s: %w", vmClassCtx, err)
	}
	defer func() {
		if err := patchHelper.Patch(ctx, vmClass); err != nil {
			if reterr == nil {
				reterr = err
			}
			vmClassCtx.Logger.Error(err, "patch failed")
		}
	}()

	if !vmClass.DeletionTimestamp.IsZero() {
		// Noop.
		return ctrl.Result{}, nil
	}

	if err := r.ReconcileNormal(vmClassCtx); err != nil {
		vmClassCtx.Logger.Error(err, "Failed to reconcile VirtualMachineClass")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *Reconciler) ReconcileNormal(vmClassCtx *pkgctx.VirtualMachineClassContext) (reterr error) {
	if pkgcfg.FromContext(vmClassCtx).Features.VMServiceClasslessVM {
		defer func() {
			class := vmClassCtx.VMClass
			condition := metav1.Condition{Type: vmopv1.VirtualMachineClassConditionReady, Status: metav1.ConditionTrue,
				ObservedGeneration: class.Generation, Reason: vmopv1.VirtualMachineClassGovernanceSatisfiedReason, Message: "Class reconciliation completed"}
			if reterr != nil {
				condition.Status = metav1.ConditionFalse
				condition.Reason = vmopv1.VirtualMachineClassReconcileErrorReason
				condition.Message = reterr.Error()
			} else {
				class.Status.ObservedGeneration = class.Generation
			}
			apimeta.SetStatusCondition(&class.Status.Conditions, condition)
		}()
		if err := r.reconcileGovernance(vmClassCtx); err != nil {
			return err
		}
	}

	// Reconcile VM class instances if the Supervisor supports it.
	if pkgcfg.FromContext(vmClassCtx).Features.ImmutableClasses {
		vmClassCtx.Logger.Info("Reconciling VirtualMachineClassInstances for VM class")
		if err := r.reconcileInstance(vmClassCtx); err != nil {
			return err
		}
	}

	return nil
}

// reconcileInstance creates a VirtualMachineClassInstance for the
// given VirtualMachineClass. The name of the instance object contains
// the a hash of ConfigSpec (and other relevant fields) to uniquely
// identify a configuration. The hash is generated using a 64-bit
// xxHash algorithm. The instance also contains:
//   - An annotation containing the same hash
//   - A label with the corresponding VM class name to allow for
//     efficient server side queries to list instances for a given VM
//     class.
//   - A label to indicate if an instance is active. An instance is
//     marked inactive if the configuration of the VM class is modified,
//     or the class is removed from the namespace or deleted.  There
//     can only be one active instance of a VM class. So, all other
//     instances are marked inactive.
//   - Owner reference to the owning class.
func (r *Reconciler) reconcileInstance(ctx *pkgctx.VirtualMachineClassContext) error {
	hash, err := vmclass.ConfigurationHash(ctx, &ctx.VMClass.Spec)
	if err != nil {
		ctx.Logger.Error(err, "Failed to calculate the hash value required to create VM class instance")
		return err
	}

	vmClassInstance := &vmopv1.VirtualMachineClassInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ctx.VMClass.Namespace,
			Name:      ctx.VMClass.Name + "-" + hash,
		},
	}

	// Note that there's a window where users will be able to specify
	// a stale VM class instance to create a VM if the call comes in
	// after we create a new instance but before we mark the existing
	// active one as "inactive". The alternative is to flip the order
	// but that will lead to rejection of VM creations even though
	// there's a valid class associated with the namespace. So we
	// choose former.
	_, err = ctrlutil.CreateOrPatch(ctx, r.Client, vmClassInstance, func() error {
		if err := ctrlutil.SetControllerReference(
			ctx.VMClass, vmClassInstance, r.Scheme()); err != nil {

			return err
		}

		vmClassInstance.Spec.VirtualMachineClassSpec = ctx.VMClass.Spec

		// Set the hash as an annotation on the instance.
		if vmClassInstance.Annotations == nil {
			vmClassInstance.Annotations = make(map[string]string)
		}
		vmClassInstance.Annotations[pkgconst.VirtualMachineClassHashAnnotationKey] = hash

		// Set a label to mark the instance as active.
		if vmClassInstance.Labels == nil {
			vmClassInstance.Labels = make(map[string]string)
		}
		vmClassInstance.Labels[vmopv1.VMClassInstanceActiveLabelKey] = ""

		return nil
	})
	if err != nil {
		return err
	}

	var list vmopv1.VirtualMachineClassInstanceList

	if err := r.Client.List(ctx, &list,
		client.InNamespace(ctx.VMClass.Namespace)); err != nil {

		ctx.Logger.Error(err, "Error listing instances for VM class")
		return err
	}

	for _, instance := range list.Items {
		// Only process instances owned by this VM class
		owned, err := ctrlutil.HasOwnerReference(instance.OwnerReferences, ctx.VMClass, r.Scheme())
		if err != nil {
			return fmt.Errorf("failed to check if VM class instance is owned by the class. err: %w", err)
		}

		if !owned {
			ctx.Logger.Info("Skipping this instance since it is not owned by VM class")
			continue
		}

		// Create a deepcopy of the object for patching.
		patch := client.MergeFrom(instance.DeepCopy())

		if val, ok := instance.Annotations[pkgconst.VirtualMachineClassHashAnnotationKey]; ok && val == hash {
			// Hash value on the VM class instance matches with the
			// hash we just calculated. Mark this class as active.
			if _, ok := instance.Labels[vmopv1.VMClassInstanceActiveLabelKey]; !ok {
				ctx.Logger.Info("Marking instance as active")
				if instance.Labels == nil {
					instance.Labels = make(map[string]string)
				}
				instance.Labels[vmopv1.VMClassInstanceActiveLabelKey] = ""
			}
		} else {
			// Mark the instance as inactive since its hash does not
			// match the hash of the current version of VM class.
			ctx.Logger.Info("Marking instance as inactive")
			delete(instance.Labels, vmopv1.VMClassInstanceActiveLabelKey)
			// If this was the last label, set labels to nil so the
			// field gets removed from the object.
		}

		if err := r.Client.Patch(ctx, &instance, patch); err != nil {
			ctx.Logger.Error(err, "patch failed")
			return err
		}
	}

	return nil
}

// classesForDependency invalidates class status on class and Zone changes.
// Periodic reconciliation also recovers from failed list calls and capability changes.
func (r *Reconciler) classesForDependency(ctx context.Context, obj client.Object) []reconcile.Request {
	namespaces := []string{obj.GetNamespace()}
	if _, ok := obj.(*topologyv1.Zone); ok {
		if system := pkgcfg.FromContext(r.Context).PodNamespace; system != "" && system != obj.GetNamespace() {
			namespaces = append(namespaces, system)
		}
	}
	var requests []reconcile.Request
	for _, namespace := range namespaces {
		var classes vmopv1.VirtualMachineClassList
		if err := r.List(ctx, &classes, client.InNamespace(namespace)); err != nil {
			r.Logger.Error(err, "list classes for governance dependency")
			continue
		}
		for i := range classes.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&classes.Items[i])})
		}
	}
	return requests
}

func (r *Reconciler) reconcileGovernance(ctx *pkgctx.VirtualMachineClassContext) error {
	var classes vmopv1.VirtualMachineClassList
	if err := r.List(ctx, &classes, client.InNamespace(ctx.VMClass.Namespace)); err != nil {
		return fmt.Errorf("list class governance: %w", err)
	}
	var zones topologyv1.ZoneList
	options := []client.ListOption{client.InNamespace(ctx.VMClass.Namespace)}
	if ctx.VMClass.Namespace == pkgcfg.FromContext(ctx).PodNamespace {
		options = nil
	}
	if err := r.List(ctx, &zones, options...); err != nil {
		return fmt.Errorf("list class zones: %w", err)
	}
	names := make([]string, 0, len(zones.Items))
	for _, z := range zones.Items {
		if z.DeletionTimestamp.IsZero() {
			names = append(names, z.Name)
		}
	}
	ctx.VMClass.Status = governance.ClassStatus(ctx.VMClass, classes.Items, names)
	return r.reconcileHardware(ctx, zones.Items, classes.Items)
}

// classesForHardware includes status-only updates: discovery refreshes do not
// change generation. Fan out to classes in every namespace, including system
// classes. Periodic reconciliation recovers from dropped events/list errors.
func (r *Reconciler) classesForHardware(ctx context.Context, _ client.Object) []reconcile.Request {
	if !pkgcfg.FromContext(r.Context).Features.VMServiceClasslessVM {
		return nil
	}
	var classes vmopv1.VirtualMachineClassList
	if err := r.List(ctx, &classes); err != nil {
		r.Logger.Error(err, "list classes for hardware discovery")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(classes.Items))
	for i := range classes.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&classes.Items[i])})
	}
	return requests
}

func (r *Reconciler) reconcileHardware(ctx *pkgctx.VirtualMachineClassContext, zones []topologyv1.Zone, classes []vmopv1.VirtualMachineClass) error {
	if len(ctx.VMClass.Status.Zones) == 0 {
		return nil
	}
	var targets vimv1.ConfigTargetList
	var options vimv1.VirtualMachineConfigOptionsList
	var guests vimv1.VirtualMachineGuestOptionsList
	for _, list := range []client.ObjectList{&targets, &options, &guests} {
		if err := r.List(ctx, list); err != nil {
			return fmt.Errorf("list class hardware discovery: %w", err)
		}
	}
	for i := range ctx.VMClass.Status.Zones {
		status := &ctx.VMClass.Status.Zones[i]
		ids := map[string]bool{}
		for _, zone := range zones {
			if zone.Name != status.Name || !zone.DeletionTimestamp.IsZero() {
				continue
			}
			for _, id := range zone.Spec.ManagedVMs.ClusterMoIDs {
				if id != "" {
					ids[id] = true
				}
			}
		}
		clusterIDs := make([]string, 0, len(ids))
		for id := range ids {
			clusterIDs = append(clusterIDs, id)
		}
		apimeta.SetStatusCondition(&status.Conditions, vmclass.HardwareCompatibility(ctx.VMClass, clusterIDs, targets.Items, options.Items, guests.Items))
		status.ResolvedLimits = vmclass.ResolveClassLimits(ctx.VMClass, classes, status.Name, clusterIDs, targets.Items, options.Items, guests.Items)
	}
	return nil
}
