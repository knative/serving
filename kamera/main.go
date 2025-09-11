package main

import (
	"context"
	"fmt"
	"log"

	// Standard Kubernetes types

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Knative Serving types and clients
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	configinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	serviceinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/service"

	// Knative controller plumbing
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	// The actual reconciler implementations
	servicereconciler "knative.dev/serving/pkg/reconciler/service"
)

// ControllerHolder is a struct to hold instantiated controllers for invocation.
type ControllerHolder struct {
	Service *controller.Impl
	Route   *controller.Impl
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

// setupControllers initializes controllers with fake clients and populates their caches.
func setupControllers(ctx context.Context, initialWorldState []runtime.Object) (*ControllerHolder, context.Context, error) {
	ctx, _ = fakeservingclient.With(ctx, initialWorldState...)

	serviceInformer := serviceinformer.Get(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	for _, obj := range initialWorldState {
		switch o := obj.(type) {
		case *v1.Service:
			if err := serviceInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, nil, fmt.Errorf("failed to add service to informer: %w", err)
			}
		case *v1.Route:
			if err := routeInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, nil, fmt.Errorf("failed to add route to informer: %w", err)
			}
		case *v1.Configuration:
			if err := configInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, nil, fmt.Errorf("failed to add configuration to informer: %w", err)
			}
		case *v1.Revision:
			if err := revisionInformer.Informer().GetIndexer().Add(o); err != nil {
				return nil, nil, fmt.Errorf("failed to add revision to informer: %w", err)
			}
		default:
			// Ignore other types for now.
		}
	}

	cmw := configmap.NewStaticWatcher()

	serviceCtrl := servicereconciler.NewController(ctx, cmw)

	return &ControllerHolder{
		Service: serviceCtrl,
	}, ctx, nil
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
	tracePath := "/path/to/your/trace.yaml"
	initialObjects, err := loadObjectsFromTrace(tracePath)
	if err != nil {
		log.Fatalf("Failed to load objects from trace: %v", err)
	}

	ctx := logging.WithLogger(context.Background(), logging.FromContext(context.Background()))

	controllers, ctx, err := setupControllers(ctx, initialObjects)
	if err != nil {
		log.Fatalf("Failed to set up controllers: %v", err)
	}

	fmt.Println("Harness initialized successfully.")
	fmt.Printf("Service controller is ready: %v\n", controllers.Service != nil)
	fmt.Println("\n--- Invoking Service Reconciler for key: default/hello-world ---")

	// This is the core invocation for a single step in your graph search.
	key := "default/hello-world"
	if err := controllers.Service.Reconciler.Reconcile(ctx, key); err != nil {
		// A requeue error is expected on the first reconcile, as status updates happen.
		if is, _ := controller.IsRequeueKey(err); is {
			fmt.Printf("Reconcile returned a requeue request (this is expected).\n")
		} else {
			log.Printf("Reconcile failed: %v", err)
		}
	} else {
		fmt.Println("Reconcile completed successfully.")
	}

	// After the reconcile, extract the writeset.
	writeset, err := extractWriteset(ctx)
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
}
