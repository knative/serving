package kamera

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	filteredinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	// Fake informers for reconcilers
	// The actual reconciler implementations
	// Import the fakes for the informers we need.
	// Import the fakes for the informers we need.
	_ "knative.dev/caching/pkg/client/injection/informers/caching/v1alpha1/image/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"

	// for kpa reconciler
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/filtered/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/factory/filtered/fake"

	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	_ "knative.dev/pkg/injection/clients/dynamicclient/fake"

	"knative.dev/serving/pkg/apis/autoscaling"
	// for cert-manager

	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/scaling"
	_ "knative.dev/serving/pkg/client/certmanager/injection/informers/acme/v1/challenge/fake"
	_ "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/certificate/fake"
	_ "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/clusterissuer/fake"
	_ "knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/service/fake"
	"knative.dev/serving/pkg/reconciler/revision/resources"

	kpareconciler "knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	certreconciler "knative.dev/serving/pkg/reconciler/certificate"
	configurationreconciler "knative.dev/serving/pkg/reconciler/configuration"
	revisionreconciler "knative.dev/serving/pkg/reconciler/revision"
	routereconciler "knative.dev/serving/pkg/reconciler/route"
	sksreconciler "knative.dev/serving/pkg/reconciler/serverlessservice"
	servicereconciler "knative.dev/serving/pkg/reconciler/service"

	"context"

	stesting "knative.dev/serving/pkg/reconciler/testing/v1"
)

func TestNewKnativeStrategy(t *testing.T) {
	tests := []struct {
		name    string
		factory ControllerFactory
	}{
		{
			name:    "Service Reconciler",
			factory: servicereconciler.NewController,
		},
		{
			name:    "Route Reconciler",
			factory: routereconciler.NewController,
		},
		{
			name:    "Revision Reconciler",
			factory: revisionreconciler.NewController,
		},
		{
			name: "KPA Reconciler",
			factory: func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
				multiScaler := scaling.NewMultiScaler(ctx.Done(), nil, logging.FromContext(ctx))
				return kpareconciler.NewController(ctx, cmw, multiScaler)
			},
		},
		{
			name:    "SKS Reconciler",
			factory: sksreconciler.NewController,
		},
		{
			name:    "Certificate Reconciler",
			factory: certreconciler.NewController,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When a fake filtered informer is imported, the testing framework expects
			// the context to be initialized with the selector key.
			// For tests that don't need a filtered informer, we can pass an empty selector list.
			selectors := []string{}
			if tt.name == "KPA Reconciler" {
				selectors = append(selectors, serving.RevisionUID)
			}
			ctx, _ := reconcilertesting.SetupFakeContext(t, func(ctx context.Context) context.Context {
				// callback ensures that we add the required selectors before SetupFakeContext proceeds.
				return filteredinformerfactory.WithSelectors(ctx, selectors...)
			})
			_, err := NewKnativeStrategy(ctx, tt.factory)
			if err != nil {
				t.Errorf("NewKnativeStrategy() error = %v", err)
			}
		})
	}
}

// setup creates a new context with fake clients and informers seeded with the given initial state.
func setup(t *testing.T, initialState []runtime.Object, selectors ...string) context.Context {
	t.Helper()
	// Create a new context with a logger.
	ctx, _ := reconcilertesting.SetupFakeContext(t, func(ctx context.Context) context.Context {
		return filteredinformerfactory.WithSelectors(ctx, selectors...)
	})

	ls := stesting.NewListers(initialState)

	// Inject fake clients into the context, seeded with the initial state.
	// This is the crucial step to ensure clients and informers are in sync.
	ctx, _ = fakeservingclient.With(ctx, ls.GetServingObjects()...)
	ctx, _ = fakekubeclient.With(ctx, ls.GetKubeObjects()...)
	ctx, _ = fakenetworkingclient.With(ctx, ls.GetNetworkingObjects()...)

	// Now, initialize the informers from the fake clients.
	ctx = filteredinformerfactory.WithSelectors(ctx, selectors...)
	ctx, informers := injection.Fake.SetupInformers(ctx, injection.GetConfig(ctx))

	// Start and sync the informers. This must be done before the reconciler is created.
	if _, err := reconcilertesting.RunAndSyncInformers(ctx, informers...); err != nil {
		t.Fatalf("Error starting and syncing informers: %v", err)
	}

	return ctx
}

func TestKnativeStrategyReconciliation(t *testing.T) {
	ns, name := "default", "test-resource"
	key := types.NamespacedName{Namespace: ns, Name: name}

	t.Run("Service Reconciler", func(t *testing.T) {
		initialState := []runtime.Object{
			&v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: v1.ServiceSpec{
					ConfigurationSpec: v1.ConfigurationSpec{
						Template: v1.RevisionTemplateSpec{
							Spec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{Image: "test"}},
								},
							},
						},
					},
					RouteSpec: v1.RouteSpec{
						Traffic: []v1.TrafficTarget{},
					},
				},
			},
		}

		ctx := setup(t, initialState)
		strategy, err := NewKnativeStrategy(ctx, servicereconciler.NewController)
		if err != nil {
			t.Fatalf("NewKnativeStrategy() error = %v", err)
		}

		reconcileAndAssertCreates(t, ctx, strategy, key, "configurations", "routes")
	})

	t.Run("Revision Reconciler", func(t *testing.T) {
		initialState := []runtime.Object{
			&v1.Revision{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Image: "test"}},
					},
				},
			},
		}
		ctx := setup(t, initialState)
		strategy, err := NewKnativeStrategy(ctx, revisionreconciler.NewController)
		if err != nil {
			t.Fatalf("NewKnativeStrategy() error = %v", err)
		}

		reconcileAndAssertCreates(t, ctx, strategy, key, "deployments", "podautoscalers", "images")
	})

	t.Run("KPA Reconciler", func(t *testing.T) {
		rev := &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{Containers: []corev1.Container{{Image: "test"}}},
			},
		}
		pa := resources.MakePA(rev, nil)
		pa.Annotations[autoscaling.ClassAnnotationKey] = autoscaling.KPA

		initialState := []runtime.Object{pa}
		ctx := setup(t, initialState, serving.RevisionUID)

		factory := func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
			multiScaler := scaling.NewMultiScaler(ctx.Done(), nil, logging.FromContext(ctx))
			return kpareconciler.NewController(ctx, cmw, multiScaler)
		}
		strategy, err := NewKnativeStrategy(ctx, factory)
		if err != nil {
			t.Fatalf("NewKnativeStrategy() error = %v", err)
		}

		reconcileAndAssertCreates(t, ctx, strategy, key, "serverlessservices")
	})

	t.Run("Configuration Reconciler", func(t *testing.T) {
		initialState := []runtime.Object{
			&v1.Configuration{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: v1.ConfigurationSpec{Template: v1.RevisionTemplateSpec{
					Spec: v1.RevisionSpec{PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{Image: "test"}},
					}}}},
			},
		}
		ctx := setup(t, initialState)
		strategy, err := NewKnativeStrategy(ctx, configurationreconciler.NewController)
		if err != nil {
			t.Fatalf("NewKnativeStrategy() error = %v", err)
		}

		reconcileAndAssertCreates(t, ctx, strategy, key, "revisions")
	})
}

// reconcileAndAssertCreates is a test helper to run a reconciliation and assert that certain resources were created.
func reconcileAndAssertCreates(t *testing.T, ctx context.Context, strategy *KnativeStrategy, key types.NamespacedName, wantCreates ...string) {
	t.Helper()
	// Promote the reconciler to leader, so it can perform actions.
	if la, ok := strategy.Controller.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	} else {
		t.Fatalf("Reconciler is not leader-aware")
	}

	// We don't need to call PrepareState because TableTest has already set up the world.
	_, err := strategy.ReconcileAtState(ctx, key)
	if requeue, _ := controller.IsRequeueKey(err); err != nil && !requeue {
		t.Fatalf("ReconcileAtState() returned an unexpected error: %v", err)
	}

	actions, err := strategy.RetrieveEffects(ctx)
	if err != nil {
		t.Fatalf("RetrieveEffects() error = %v", err)
	}

	created := make(map[string]bool)
	for _, action := range actions {
		fmt.Println("Action:", action.GetVerb(), action.GetResource().Resource)
		if action.GetVerb() == "create" {
			created[action.GetResource().Resource] = true
		}
	}

	for _, want := range wantCreates {
		if !created[want] {
			t.Errorf("Expected a resource of type %q to be created, but it wasn't.", want)
		}
	}
}
