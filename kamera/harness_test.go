package kamera

import (
	"testing"

	reconcilertesting "knative.dev/pkg/reconciler/testing"

	// The actual reconciler implementations
	// Import the fakes for the informers we need.
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/service/fake"

	routereconciler "knative.dev/serving/pkg/reconciler/route"
	servicereconciler "knative.dev/serving/pkg/reconciler/service"
)

func TestNewKnativeStrategy(t *testing.T) {
	// The KPA reconciler needs a MultiScaler. We can create a fake one for initialization.
	// multiScaler := scaling.NewMultiScaler(context.Background().Done(), nil, logging.FromContext(context.Background()))

	tests := []struct {
		name    string
		factory ControllerFactory
		wantErr bool
	}{
		{
			name:    "Service Reconciler",
			factory: servicereconciler.NewController,
		},
		{
			name:    "Route Reconciler",
			factory: routereconciler.NewController,
		},
		// {
		// 	name:    "Revision Reconciler",
		// 	factory: revisionreconciler.NewController,
		// },
		// {
		// 	name: "KPA Reconciler",
		// 	factory: func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		// 		return kpareconciler.NewController(ctx, cmw, multiScaler)
		// 	},
		// },
		// {
		// 	name:    "SKS Reconciler",
		// 	factory: sksreconciler.NewController,
		// },
		// {
		// 	name: "Certificate Reconciler",
		// 	factory: func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		// 		return certreconciler.NewController(ctx, cmw)
		// 	},
		// },
		// {
		// 	name:    "Nil factory",
		// 	factory: nil,
		// 	wantErr: true,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := reconcilertesting.SetupFakeContext(t) // injects the informers
			_, err := NewKnativeStrategy(ctx, tt.factory)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewKnativeStrategy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
