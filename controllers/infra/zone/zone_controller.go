// © Broadcom. All Rights Reserved.
// The term “Broadcom” refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package zone

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	topologyv1 "github.com/vmware-tanzu/vm-operator/external/tanzu-topology/api/v1alpha1"
	vimv1 "github.com/vmware-tanzu/vm-operator/external/vim/api/v1alpha1"
	pkgcfg "github.com/vmware-tanzu/vm-operator/pkg/config"
	pkgctx "github.com/vmware-tanzu/vm-operator/pkg/context"
	"github.com/vmware-tanzu/vm-operator/pkg/governance"
	pkglog "github.com/vmware-tanzu/vm-operator/pkg/log"
	"github.com/vmware-tanzu/vm-operator/pkg/patch"
	"github.com/vmware-tanzu/vm-operator/pkg/record"
	"github.com/vmware-tanzu/vm-operator/pkg/util/vsphere/watcher"
	vimtypes "github.com/vmware/govmomi/vim25/types"
)

// SkipNameValidation is used for testing to allow multiple controllers with the
// same name since Controller-Runtime has a global singleton registry to
// prevent controllers with the same name, even if attached to different
// managers.
var SkipNameValidation *bool

// AddToManager adds this package's controller to the provided manager.
func AddToManager(ctx *pkgctx.ControllerManagerContext, mgr manager.Manager) error {
	var (
		controlledType     = &topologyv1.Zone{}
		controlledTypeName = reflect.TypeFor[topologyv1.Zone]().Name()

		controllerNameShort = fmt.Sprintf("%s-controller", strings.ToLower(controlledTypeName))
	)

	r := NewReconciler(
		ctx,
		mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName(controlledTypeName),
		record.New(mgr.GetEventRecorder(controllerNameShort)),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(controlledType).
		Watches(&vmopv1.VirtualMachineClass{}, handler.EnqueueRequestsFromMapFunc(r.zonesForClass), builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || !e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp())
			},
		})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: ctx.GetMaxConcurrentReconciles(controllerNameShort, 0),
			SkipNameValidation:      SkipNameValidation,
			LogConstructor:          pkglog.ControllerLogConstructor(controllerNameShort, controlledType, mgr.GetScheme()),
		}).
		Complete(r)
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

// Finalizer is the finalizer placed on Zone objects by VM Operator.
const Finalizer = "vmoperator.vmware.com/zone-finalizer"

// Reconciler reconciles a StoragePolicyQuota object.
type Reconciler struct {
	client.Client

	Context  context.Context
	Logger   logr.Logger
	Recorder record.Recorder
}

// +kubebuilder:rbac:groups=topology.tanzu.vmware.com,resources=zones,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=topology.tanzu.vmware.com,resources=zones/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vim.vmware.com,resources=configtargets,verbs=get;list;watch;create;update;patch

func (r *Reconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx = pkgcfg.JoinContext(ctx, r.Context)
	ctx = watcher.JoinContext(ctx, r.Context)

	var obj topologyv1.Zone

	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patchHelper, err := patch.NewHelper(&obj, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		if err := patchHelper.Patch(ctx, &obj); err != nil {
			if reterr == nil {
				reterr = err
			} else {
				reterr = fmt.Errorf("%w,%w", err, reterr)
			}
		}
	}()

	if !obj.DeletionTimestamp.IsZero() {
		return r.ReconcileDelete(ctx, &obj)
	}

	result, err := r.ReconcileNormal(ctx, &obj)
	if err == nil && result.RequeueAfter == 0 {
		result.RequeueAfter = time.Minute
	}
	return result, err
}

func (r *Reconciler) ReconcileDelete(
	ctx context.Context,
	obj *topologyv1.Zone) (ctrl.Result, error) {
	if val := obj.Spec.ManagedVMs.FolderMoID; val != "" {
		if err := watcher.Remove(
			ctx,
			vimtypes.ManagedObjectReference{
				Type:  "Folder",
				Value: val,
			},
			fmt.Sprintf("%s/%s", obj.Namespace, obj.Name)); err != nil {

			if !errors.Is(err, watcher.ErrAsyncSignalDisabled) {
				// We don't ignore watcher.ErrNoWatcher here to interlock with the vm watcher
				// service that is in the process of restarting the watcher. This does mean
				// that if watcher cannot start like because of invalid VC creds the finalizer
				// won't be removed.
				return ctrl.Result{}, err
			}
		}
	}

	controllerutil.RemoveFinalizer(obj, Finalizer)

	return ctrl.Result{}, nil
}

func (r *Reconciler) ReconcileNormal(
	ctx context.Context,
	obj *topologyv1.Zone) (ctrl.Result, error) {
	if controllerutil.AddFinalizer(obj, Finalizer) {
		// Ensure the finalizer is present before watching this zone.
		return ctrl.Result{}, nil
	}

	var errs []error

	if val := obj.Spec.ManagedVMs.FolderMoID; val != "" {
		if err := watcher.Add(
			ctx,
			vimtypes.ManagedObjectReference{
				Type:  "Folder",
				Value: val,
			},
			fmt.Sprintf("%s/%s", obj.Namespace, obj.Name)); err != nil {

			if !errors.Is(err, watcher.ErrAsyncSignalDisabled) {
				errs = append(errs, err)
			}
		}
	}

	if pkgcfg.FromContext(ctx).Features.VMServiceClasslessVM {
		if err := r.reconcileGoverningClasses(ctx, obj); err != nil {
			errs = append(errs, err)
		}
		if err := r.reconcileConfigTargets(ctx, obj); err != nil {
			errs = append(errs, err)
		}
	}

	return ctrl.Result{}, errors.Join(errs...)
}

// reconcileConfigTargets ensures a cluster-scoped ConfigTarget exists for each
// ClusterComputeResource MoID in zone.spec.managedVMs.clusterMoIDs.
func (r *Reconciler) reconcileConfigTargets(
	ctx context.Context,
	zone *topologyv1.Zone) error {
	for _, clusterMoID := range zone.Spec.ManagedVMs.ClusterMoIDs {
		ct := &vimv1.ConfigTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterMoID,
			},
		}

		if _, err := controllerutil.CreateOrPatch(ctx, r.Client, ct, func() error {
			// clusterMoID also derives ct.Name (set above), so spec.id can
			// never differ across reconciles for the same object; setting it
			// unconditionally is a no-op on update.
			ct.Spec.ID = vimv1.ManagedObjectID{ID: clusterMoID}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to reconcile ConfigTarget %s: %w", clusterMoID, err)
		}
	}

	return nil
}

// zonesForClass rechecks all namespace Zones when class availability or governance changes.
func (r *Reconciler) zonesForClass(ctx context.Context, obj client.Object) []reconcile.Request {
	var zones topologyv1.ZoneList
	if err := r.List(ctx, &zones, client.InNamespace(obj.GetNamespace())); err != nil {
		r.Logger.Error(err, "list Zones for class governance")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(zones.Items))
	for i := range zones.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&zones.Items[i])})
	}
	return requests
}

func (r *Reconciler) reconcileGoverningClasses(ctx context.Context, obj *topologyv1.Zone) error {
	var classes vmopv1.VirtualMachineClassList
	if err := r.List(ctx, &classes, client.InNamespace(obj.Namespace)); err != nil {
		return fmt.Errorf("list Zone governing classes: %w", err)
	}
	names := governance.GoverningNames(classes.Items, obj.Namespace, obj.Name)
	if slices.Equal(obj.Status.GoverningClasses, names) {
		return nil
	}
	// Patch an isolated object. Leaving obj unchanged prevents the deferred
	// patch helper from replaying this list without a resource-version lock.
	next := obj.DeepCopy()
	next.Status.GoverningClasses = names
	if err := r.Status().Patch(ctx, next, client.MergeFromWithOptions(obj.DeepCopy(), client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("patch Zone governance mirror: %w", err)
	}
	return nil
}
