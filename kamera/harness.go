package kamera

import (
	"context"
	"fmt"
	"time"

	// Standard Kubernetes types

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Knative Serving types and clients
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalercfg "knative.dev/serving/pkg/autoscaler/config"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	configinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	serviceinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/service"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"

	netcfg "knative.dev/networking/pkg/config"

	// Knative pkg imports
	// Knative controller plumbing
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/system"

	// The actual reconciler implementations
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	cfgmap "knative.dev/serving/pkg/apis/config"
)

type Result struct {
	Requeue      bool
	RequeueAfter time.Duration // in seconds
}

type Strategy interface {
	PrepareState(ctx context.Context, state []runtime.Object) (context.Context, error)
	ReconcileAtState(ctx context.Context, nsName types.NamespacedName) (Result, error)
	RetrieveEffects(ctx context.Context) ([]testing.Action, error)
}

// ControllerFactory is a function that creates a new controller.
type ControllerFactory func(ctx context.Context, cmw configmap.Watcher) *controller.Impl

// KnativeStrategy implements the Strategy interface for Knative controllers.
type KnativeStrategy struct {
	Controller *controller.Impl
}

// NewKnativeStrategy creates a new KnativeStrategy for a given controller factory.
func NewKnativeStrategy(ctx context.Context, factory ControllerFactory) (*KnativeStrategy, error) {
	if factory == nil {
		return nil, fmt.Errorf("controller factory cannot be nil")
	}

	// The context passed in from the test should already have fakes injected.

	cmw := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.DefaultsConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalercfg.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	},
		// added the following for route reconciler
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.DomainConfigName,
				Namespace: system.Namespace(),
			},
			Data: map[string]string{
				"test-domain.dev": "",
				"prod-domain.com": "selector:\n  app: prod",
			},
		}, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      netcfg.ConfigMapName,
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		}, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gc.ConfigName,
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		}, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfgmap.FeaturesConfigName,
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		},
		// added for revision informer
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-observability",
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-deployment",
				Namespace: system.Namespace(),
			},
			Data: map[string]string{
				"queue-sidecar-image": "gcr.io/knative-releases/knative.dev/serving/cmd/queue@sha256:abc123",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-logging",
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		},
	)

	ctrl := factory(ctx, cmw)

	return &KnativeStrategy{
		Controller: ctrl,
	}, nil
}

// PrepareState sets up the fake clients and informers for the reconciler under test.
func (ks *KnativeStrategy) PrepareState(ctx context.Context, state []runtime.Object) (context.Context, error) {
	return setupControllerState(ctx, state)
}

// ReconcileAtState invokes the reconciler for a given state.
func (ks *KnativeStrategy) ReconcileAtState(ctx context.Context, nsName types.NamespacedName) (Result, error) {
	// The controller is now initialized in the constructor.
	key := nsName.String()
	err := ks.Controller.Reconciler.Reconcile(ctx, key)

	requeue, requeueAfter := controller.IsRequeueKey(err)
	if err != nil && !requeue {
		return Result{}, err // Return actual error if it's not a requeue request
	}

	return Result{
		Requeue:      requeue,
		RequeueAfter: requeueAfter,
	}, nil
}

// RetrieveEffects extracts the results of a reconciliation.
func (ks *KnativeStrategy) RetrieveEffects(ctx context.Context) ([]testing.Action, error) {
	client := fakeservingclient.Get(ctx)
	if client == nil {
		return nil, fmt.Errorf("fake serving client not found in context")
	}
	return client.Actions(), nil
}

// loadObjectsFromTrace is a stub function.
func loadObjectsFromTrace(tracePath string) ([]runtime.Object, error) {
	fmt.Printf("Stub: Loading objects from trace file at %s\n", tracePath)
	return []runtime.Object{
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hello-world",
				Namespace: "default",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{
						Spec: v1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Image: "gcr.io/knative-samples/helloworld-go",
								}},
							},
						},
					},
				},
			},
		},
	}, nil
}

// setupControllerState initializes the context with fake clients and populates their informer caches.
func setupControllerState(ctx context.Context, initialWorldState []runtime.Object) (context.Context, error) {
	ctx, _ = fakeservingclient.With(ctx, initialWorldState...)

	serviceInformer := serviceinformer.Get(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)
	paInformer := painformer.Get(ctx)
	metricInformer := metricinformer.Get(ctx)

	for _, obj := range initialWorldState {
		switch o := obj.(type) {
		case *v1.Service:
			if err := serviceInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add service to informer: %w", err)
			}
		case *v1.Route:
			if err := routeInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add route to informer: %w", err)
			}
		case *v1.Configuration:
			if err := configInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add configuration to informer: %w", err)
			}
		case *v1.Revision:
			if err := revisionInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add revision to informer: %w", err)
			}
		case *autoscalingv1alpha1.PodAutoscaler:
			if err := paInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add podautoscaler to informer: %w", err)
			}
		case *autoscalingv1alpha1.Metric:
			if err := metricInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, fmt.Errorf("failed to add metric to informer: %w", err)
			}
		default:
			// Ignore other types for now.
		}
	}
	return ctx, nil
}

// extractWriteset inspects the fake client's actions and returns the created/updated objects.
func extractWriteset(ctx context.Context) ([]runtime.Object, error) {
	client := fakeservingclient.Get(ctx)
	if client == nil {
		return nil, fmt.Errorf("fake serving client not found in context")
	}

	actions := client.Actions()
	writeset := make([]runtime.Object, 0, len(actions))

	for _, action := range actions {
		var obj runtime.Object
		switch act := action.(type) {
		case testing.CreateActionImpl:
			obj = act.GetObject()
		case testing.UpdateActionImpl:
			obj = act.GetObject()
		default:
			// Skip non-mutating actions like Get, List, Watch
			continue
		}
		writeset = append(writeset, obj)
	}
	return writeset, nil
}
