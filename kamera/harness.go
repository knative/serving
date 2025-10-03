package kamera

import (
	"context"
	"fmt"
	"time"

	// Standard Kubernetes types

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	test "k8s.io/client-go/testing"
	filteredinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/reconciler"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Knative Serving types and clients
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
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
	RetrieveEffects(ctx context.Context) ([]test.Action, error)
}

// ControllerFactory is a function that creates a new controller.
type ControllerFactory func(ctx context.Context, cmw configmap.Watcher) *controller.Impl

// KnativeStrategy implements the Strategy interface for Knative controllers.
type KnativeStrategy struct {
	factory   ControllerFactory
	selectors []string
}

// NewKnativeStrategy creates a new KnativeStrategy for a given controller factory.
func NewKnativeStrategy(factory ControllerFactory, selectors ...string) (*KnativeStrategy, error) {
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

		// added for certificate reconciler
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-certmanager",
				Namespace: system.Namespace(),
			},
			Data: map[string]string{},
		},
	)

	partial := func(ctx context.Context, _ configmap.Watcher) *controller.Impl {
		return factory(ctx, cmw)
	}

	return &KnativeStrategy{
		factory:   partial,
		selectors: selectors,
	}, nil
}

// PrepareState sets up the fake clients and informers for the reconciler under test.
func (ks *KnativeStrategy) PrepareState(ctx context.Context, state []runtime.Object) (context.Context, error) {
	ctx, _, err := SetupClientState(ctx, state, ks.selectors...)
	return ctx, err
}

// ReconcileAtState invokes the reconciler for a given state.
func (ks *KnativeStrategy) ReconcileAtState(ctx context.Context, nsName types.NamespacedName) (Result, error) {
	// must re-initialize the controller each time to reset its informer state
	ctrl := ks.factory(ctx, nil)
	if la, ok := ctrl.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	} else {
		return Result{}, fmt.Errorf("Reconciler is not leader-aware")
	}

	key := nsName.String()
	err := ctrl.Reconciler.Reconcile(ctx, key)

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
func (ks *KnativeStrategy) RetrieveEffects(ctx context.Context) ([]test.Action, error) {
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

func SetupClientState(ctx context.Context, state []runtime.Object, selectors ...string) (context.Context, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	ctx = filteredinformerfactory.WithSelectors(ctx, selectors...)
	ctx = injection.WithConfig(ctx, &rest.Config{})
	ctx, informers := injection.Fake.SetupInformers(ctx, &rest.Config{})

	if err := insertObjects(ctx, state); err != nil {
		return nil, cancel, err
	}

	waitInformers, err := reconcilertesting.RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to sync informers: %w", err)
	}

	return ctx, func() {
		cancel()
		waitInformers()
	}, nil
}

func insertObjects(ctx context.Context, objs []runtime.Object) error {
	servingclient := fakeservingclient.Get(ctx)
	kubeclient := fakekubeclient.Get(ctx)

	// i am sorry for the following code
	for _, obj := range objs {
		switch o := obj.(type) {
		case *v1.Service:
			if _, err := servingclient.ServingV1().Services(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}
		case *v1.Route:
			if _, err := servingclient.ServingV1().Routes(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create route: %w", err)
			}
		case *v1.Configuration:
			if _, err := servingclient.ServingV1().Configurations(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create configuration: %w", err)
			}
		case *v1.Revision:
			if _, err := servingclient.ServingV1().Revisions(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create revision: %w", err)
			}
		case *autoscalingv1alpha1.PodAutoscaler:
			if _, err := servingclient.AutoscalingV1alpha1().PodAutoscalers(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create podautoscaler: %w", err)
			}
		case *autoscalingv1alpha1.Metric:
			if _, err := servingclient.AutoscalingV1alpha1().Metrics(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create metric: %w", err)
			}
		case *corev1.ConfigMap:
			if _, err := kubeclient.CoreV1().ConfigMaps(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create configmap: %w", err)
			}
		case *corev1.Secret:
			if _, err := kubeclient.CoreV1().Secrets(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create secret: %w", err)
			}
		case *corev1.ServiceAccount:
			if _, err := kubeclient.CoreV1().ServiceAccounts(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create serviceaccount: %w", err)
			}
		case *corev1.Pod:
			if _, err := kubeclient.CoreV1().Pods(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create pod: %w", err)
			}
		case *corev1.Endpoints:
			if _, err := kubeclient.CoreV1().Endpoints(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create endpoints: %w", err)
			}
		case *corev1.Service:
			if _, err := kubeclient.CoreV1().Services(o.Namespace).Create(ctx, o, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}
		default:
			panic("unsupported type -- need to add a case for it")
		}
	}
	return nil

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
		case test.CreateActionImpl:
			obj = act.GetObject()
		case test.UpdateActionImpl:
			obj = act.GetObject()
		default:
			// Skip non-mutating actions like Get, List, Watch
			continue
		}
		writeset = append(writeset, obj)
	}
	return writeset, nil
}
