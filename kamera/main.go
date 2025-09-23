package main

import (
	"context"
	"fmt"
	"log"
	"time"

	// Standard Kubernetes types

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Knative Serving types and clients
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	configinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	serviceinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/service"

	// Knative controller plumbing
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	// The actual reconciler implementations
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/scaling"
	kpareconciler "knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	certreconciler "knative.dev/serving/pkg/reconciler/certificate"
	revisionreconciler "knative.dev/serving/pkg/reconciler/revision"
	routereconciler "knative.dev/serving/pkg/reconciler/route"
	sksreconciler "knative.dev/serving/pkg/reconciler/serverlessservice"
	servicereconciler "knative.dev/serving/pkg/reconciler/service"
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
func NewKnativeStrategy(factory ControllerFactory) (*KnativeStrategy, error) {
	if factory == nil {
		return nil, fmt.Errorf("controller factory cannot be nil")
	}

	// This context is temporary, used only for initialization.
	ctx := logging.WithLogger(context.Background(), logging.FromContext(context.Background()))
	ctx, _ = fakeservingclient.With(ctx) // Initialize with an empty fake client

	cmw := configmap.NewStaticWatcher()
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

func main() {
	tracePath := "/path/to/tracefile.json"
	initialObjects, err := loadObjectsFromTrace(tracePath)
	if err != nil {
		log.Fatalf("Failed to load objects from trace: %v", err)
	}

	ctx := logging.WithLogger(context.Background(), logging.FromContext(context.Background()))

	// --- Service Controller Reconciliation ---
	// Create a strategy for the Service reconciler.
	serviceStrategy, err := NewKnativeStrategy(servicereconciler.NewController)
	if err != nil {
		log.Fatalf("Failed to create KnativeStrategy: %v", err)
	}

	serviceCtx, err := serviceStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare state: %v", err)
	}

	fmt.Println("Harness initialized successfully.")
	fmt.Printf("Service controller is ready: %v\n", serviceStrategy.Controller != nil)
	fmt.Println("\n--- Invoking Service Reconciler for key: default/hello-world ---")

	nsName := types.NamespacedName{Namespace: "default", Name: "hello-world"}
	if result, err := serviceStrategy.ReconcileAtState(serviceCtx, nsName); err != nil {
		log.Printf("Reconcile failed: %v", err)
	} else {
		if result.Requeue {
			fmt.Printf("Reconcile returned a requeue request (this is expected).\n")
		} else {
			fmt.Println("Reconcile completed successfully.")
		}
	}

	// After the reconcile, extract the writeset.
	writeset, err := extractWriteset(serviceCtx)
	if err != nil {
		log.Fatalf("Failed to extract writeset: %v", err)
	}

	fmt.Printf("\n--- Captured Writeset (%d objects) ---\n", len(writeset))
	for i, obj := range writeset {
		// Use sigs.k8s.io/yaml for pretty printing.
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			log.Printf("Failed to marshal object %d: %v", i, err)
			continue
		}
		fmt.Printf("-- Object %d --\n", i+1)
		fmt.Println(string(bytes))
	}

	// --- PodAutoscaler Controller Reconciliation ---
	// The KPA reconciler needs a MultiScaler. We can create a fake one.
	multiScaler := scaling.NewMultiScaler(ctx.Done(), nil, logging.FromContext(ctx))

	// Wrap the KPA controller constructor to match our ControllerFactory signature.
	kpaControllerFactory := func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		return kpareconciler.NewController(ctx, cmw, multiScaler)
	}

	// Create a strategy for the PodAutoscaler reconciler.
	kpaStrategy, err := NewKnativeStrategy(kpaControllerFactory)
	if err != nil {
		log.Fatalf("Failed to create KPA KnativeStrategy: %v", err)
	}

	// The PA is created by the Revision controller, which is not run here.
	// For this test, we'll add the PA that the Revision controller would have created.
	// initialObjects = append(initialObjects, resources.MakePA(initialObjects[0].(*v1.Service), nil))
	// The PA is created by the Revision controller. To test the KPA reconciler
	// independently, we would need to manually construct a PodAutoscaler object
	// and add it to initialObjects. For this PoC, we'll focus on controllers
	// whose inputs are more readily available from the initial Service.
	// For example:
	// initialObjects = append(initialObjects, revisionresources.MakePA(revisionObject, nil))

	kpaCtx, err := kpaStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare KPA state: %v", err)
	}

	fmt.Printf("\nPodAutoscaler controller is ready: %v\n", kpaStrategy.Controller != nil)
	fmt.Println("\n--- Invoking PodAutoscaler Reconciler for key: default/hello-world ---")

	// The PA has the same name as the Service in this context.
	if _, err := kpaStrategy.ReconcileAtState(kpaCtx, nsName); err != nil {
		log.Printf("KPA Reconcile failed: %v", err)
	} else {
		fmt.Println("KPA Reconcile completed successfully.")
	}

	// --- Revision Controller Reconciliation ---
	revisionStrategy, err := NewKnativeStrategy(revisionreconciler.NewController)
	if err != nil {
		log.Fatalf("Failed to create Revision KnativeStrategy: %v", err)
	}
	revisionCtx, err := revisionStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare Revision state: %v", err)
	}
	fmt.Printf("Revision context ready: %v\n", revisionCtx != nil)
	fmt.Printf("\nRevision controller is ready: %v\n", revisionStrategy.Controller != nil)
	// Note: The revision controller is typically triggered by changes to a Configuration,
	// which is created by the Service controller. A full end-to-end flow would
	// require chaining these reconciliations.

	// --- ServerlessService (SKS) Controller Reconciliation ---
	sksStrategy, err := NewKnativeStrategy(sksreconciler.NewController)
	if err != nil {
		log.Fatalf("Failed to create SKS KnativeStrategy: %v", err)
	}
	sksCtx, err := sksStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare SKS state: %v", err)
	}
	fmt.Printf("SKS context ready: %v\n", sksCtx != nil)
	fmt.Printf("\nServerlessService controller is ready: %v\n", sksStrategy.Controller != nil)
	// Note: The SKS controller is triggered by PodAutoscaler changes.

	// --- Certificate Controller Reconciliation ---
	// The certificate reconciler constructor has a different signature.
	certControllerFactory := func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		return certreconciler.NewController(ctx, cmw)
	}
	certStrategy, err := NewKnativeStrategy(certControllerFactory)
	if err != nil {
		log.Fatalf("Failed to create Certificate KnativeStrategy: %v", err)
	}
	certCtx, err := certStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare Certificate state: %v", err)
	}
	fmt.Printf("Certificate context ready: %v\n", certCtx != nil)
	fmt.Printf("\nCertificate controller is ready: %v\n", certStrategy.Controller != nil)
	// Note: The Certificate controller is triggered by Route changes.

	// --- Route Controller Reconciliation ---
	routeStrategy, err := NewKnativeStrategy(routereconciler.NewController)
	if err != nil {
		log.Fatalf("Failed to create Route KnativeStrategy: %v", err)
	}
	routeCtx, err := routeStrategy.PrepareState(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to prepare Route state: %v", err)
	}
	fmt.Printf("Route context ready: %v\n", routeCtx != nil)
	fmt.Printf("\nRoute controller is ready: %v\n", routeStrategy.Controller != nil)
	// Note: The Route controller is triggered by Service changes.

	fmt.Println("\n--- All controller strategies instantiated ---")
}
