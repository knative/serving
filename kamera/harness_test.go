package kamera

import (
	"context"
	"testing"

	filteredinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

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

	// for cert-manager
	"knative.dev/serving/pkg/apis/serving"
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

	logging "knative.dev/pkg/logging"

	kpareconciler "knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	certreconciler "knative.dev/serving/pkg/reconciler/certificate"
	revisionreconciler "knative.dev/serving/pkg/reconciler/revision"
	routereconciler "knative.dev/serving/pkg/reconciler/route"
	sksreconciler "knative.dev/serving/pkg/reconciler/serverlessservice"
	servicereconciler "knative.dev/serving/pkg/reconciler/service"
)

func TestNewKnativeStrategy(t *testing.T) {
	// The KPA reconciler needs a MultiScaler. We can create a fake one for initialization.
	// multiScaler := scaling.NewMultiScaler(context.Background().Done(), nil, logging.FromContext(context.Background()))

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
				return filteredinformerfactory.WithSelectors(ctx, selectors...)
			})
			_, err := NewKnativeStrategy(ctx, tt.factory)
			if err != nil {
				t.Errorf("NewKnativeStrategy() error = %v", err)
			}
		})
	}
}
