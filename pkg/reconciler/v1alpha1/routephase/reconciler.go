/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package routephase

import (
	"context"
	"reflect"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servingclientset "github.com/knative/serving/pkg/client/clientset/versioned"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	reconcilerv1alpha1 "github.com/knative/serving/pkg/reconciler/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/routephase/phase"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type RoutePhase interface {
	reconciler.Phase
	Reconcile(ctx context.Context, route *v1alpha1.Route) (v1alpha1.RouteStatus, error)
}

type Reconciler struct {
	reconciler.CommonOptions

	RouteLister      servinglisters.RouteLister
	ServingClientset servingclientset.Interface

	Configs     reconciler.ConfigStore
	RoutePhases []RoutePhase
}

func New(opts reconciler.CommonOptions, deps *reconcilerv1alpha1.DependencyFactory) reconciler.Reconciler {
	store := config.NewStore(opts.Logger.Named("config-store"))
	store.WatchConfigs(opts.ConfigMapWatcher)

	return &Reconciler{
		CommonOptions: opts,

		RouteLister:      deps.Serving.InformerFactory.Serving().V1alpha1().Routes().Lister(),
		ServingClientset: deps.Serving.Client,

		Configs: store,
		RoutePhases: []RoutePhase{
			phase.NewDomain(opts, deps),
			phase.NewK8sService(opts, deps),
			phase.NewVirtualService(opts, deps),
		},
	}

}

func (r *Reconciler) ConfigStore() reconciler.ConfigStore {
	return r.Configs
}

func (r *Reconciler) Phases() []reconciler.Phase {
	var phases []reconciler.Phase

	for _, routePhase := range r.RoutePhases {
		phases = append(phases, routePhase)
	}

	return phases
}

func (r *Reconciler) Triggers() []reconciler.Trigger {
	return []reconciler.Trigger{{
		ObjectKind:  v1alpha1.SchemeGroupVersion.WithKind("Route"),
		EnqueueType: reconciler.EnqueueObject,
	}}
}

func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	ctx = r.Configs.ToContext(ctx)
	logger := logging.FromContext(ctx)

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		r.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the Route resource with this namespace/name
	original, err := r.RouteLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("route %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	route := original.DeepCopy()

	// Reconcile this copy of the route and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = r.reconcilePhases(ctx, logger, route)
	if equality.Semantic.DeepEqual(original.Status, route.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := r.updateStatus(ctx, route); err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		r.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for route %q: %v", route.Name, err)
		return err
	}
	return err
}

func (r *Reconciler) reconcilePhases(ctx context.Context, logger *zap.SugaredLogger, route *v1alpha1.Route) error {
	route.Status.InitializeConditions()

	for _, phase := range r.RoutePhases {
		status, rerr := phase.Reconcile(ctx, route)

		if err := route.Status.MergePartial(&status); err != nil {
			// TODO(dprtoaso) Consider failing and returning an error
			//
			// Merge fails when argument types are different - this shouldn't happen (for route status)
			// or
			// when a provided transformer returns an error - this shouldn't happen (for route status)
			logger.Warnf("merging route status failed: ", err)
		}

		if rerr != nil {
			return rerr
		}
	}
	return nil
}

// Update the Status of the route.  Caller is responsible for checking
// for semantic differences before calling.
func (r *Reconciler) updateStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	existing, err := r.RouteLister.Routes(route.Namespace).Get(route.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(existing.Status, route.Status) {
		return existing, nil
	}
	existing.Status = route.Status
	// TODO: for CRD there's no updatestatus, so use normal update.
	updated, err := r.ServingClientset.ServingV1alpha1().Routes(route.Namespace).Update(existing)
	if err != nil {
		return nil, err
	}

	r.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated status for route %q", route.Name)
	return updated, nil
}
