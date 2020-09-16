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

package hpa

import (
	"context"
	"errors"
	"testing"

	// Inject our fake informers
	networkingclient "knative.dev/networking/pkg/client/injection/client"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/autoscaling/v2beta1/horizontalpodautoscaler/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	_ "knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	fakepainformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"

	"knative.dev/networking/pkg/apis/networking"
	nv1a1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	asconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
	aresources "knative.dev/serving/pkg/reconciler/autoscaling/resources"

	_ "knative.dev/pkg/metrics/testing"
	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestControllerCanReconcile(t *testing.T) {
	ctx, cancel, infs := SetupFakeContextWithCancel(t)
	ctl := NewController(ctx, configmap.NewStaticWatcher(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      asconfig.ConfigName,
			},
			Data: map[string]string{},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      deployment.ConfigName,
			},
			Data: map[string]string{
				deployment.QueueSidecarImageKey: "motorbike-sidecar",
			},
		}))

	waitInformers, err := controller.RunInformers(ctx.Done(), infs...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	podAutoscaler := pa(testNamespace, testRevision, WithHPAClass)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(ctx, podAutoscaler, metav1.CreateOptions{})
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(podAutoscaler)

	// The Reconciler won't do any work until it becomes the leader.
	if la, ok := ctl.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	}

	err = ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision)
	if err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	_, err = fakekubeclient.Get(ctx).AutoscalingV2beta1().HorizontalPodAutoscalers(testNamespace).Get(ctx, testRevision, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting hpa: %v", err)
	}
}

func TestReconcile(t *testing.T) {
	retryAttempted := false
	const (
		deployName = testRevision + "-deployment"
		privateSvc = testRevision + "-private"
	)

	table := TableTest{{
		Name: "no op",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithPASKSReady, WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc), withScales(0, 0)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "create hpa & sks, with retry",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WithReactors: []ktesting.ReactionFunc{
			func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "podautoscalers") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				return true, nil, apierrs.NewConflict(v1.Resource("foo"), "bar", errors.New("foo"))
			},
		},
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0),
				WithTraffic, WithPASKSNotReady("SKS Services are not ready yet")),
		}, {
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0),
				WithTraffic, WithPASKSNotReady("SKS Services are not ready yet")),
		}},
	}, {
		Name: "reconcile sks is still not ready",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithPubService,
				WithPrivateService),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithTraffic, withScales(0, 0),
				WithPASKSNotReady("SKS Services are not ready yet"), WithTraffic,
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "reconcile sks becomes ready",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithPASKSNotReady("I wasn't ready yet :-("),
				WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0),
				WithPASKSReady, WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "reconcile sks",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu")), withHPAScaleStatus(5, 3)),
			pa(testNamespace, testRevision, WithHPAClass, withScales(1, 4), WithPASKSNotReady("crufty"), WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef("bar"), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithPASKSReady, WithTraffic,
				WithScaleTargetInitialized, withScales(5, 3),
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		}},
	}, {
		Name: "reconcile unhappy sks",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName+"-hairy"),
				WithPubService, WithPrivateService),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0),
				WithPASKSNotReady("SKS Services are not ready yet"), WithTraffic,
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName),
				WithPubService, WithPrivateService),
		}},
	}, {
		Name: "reconcile sks - update fails",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic, withScales(0, 0)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef("bar"), WithSKSReady),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(testNamespace, testRevision),
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		WantErr: true,
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: error updating SKS test-revision: inducing failure for update serverlessservices"),
		},
	}, {
		Name: "create sks - create fails",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0), WithTraffic),
			deploy(testNamespace, testRevision),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(testNamespace, testRevision),
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("create", "serverlessservices"),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: error creating SKS test-revision: inducing failure for create serverlessservices"),
		},
	}, {
		Name: "sks is disowned",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSOwnersRemoved, WithSKSReady),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key:     key(testNamespace, testRevision),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, MarkResourceNotOwnedByPA("ServerlessService", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `error reconciling SKS: PA: test-revision does not own SKS: test-revision`),
		},
	}, {
		Name: "pa is disowned",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"), WithPAOwnersRemoved), withHPAOwnersRemoved),
		},
		Key:     key(testNamespace, testRevision),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, MarkResourceNotOwnedByPA("HorizontalPodAutoscaler", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`PodAutoscaler: "test-revision" does not own HPA: "test-revision"`),
		},
	}, {
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithPADeletionTimestamp),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "update pa fails",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu")), withHPAScaleStatus(19, 18)),
			pa(testNamespace, testRevision, WithHPAClass, WithPAStatusService("the-wrong-one"), withScales(42, 84)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(19, 18),
				WithPASKSReady, WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc)),
		}},
		Key:     key(testNamespace, testRevision),
		WantErr: true,
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("update", "podautoscalers"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "test-revision": inducing failure for update podautoscalers`),
		},
	}, {
		Name: "update hpa fails",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithPASKSReady, WithScaleTargetInitialized,
				WithTraffic, withScales(0, 0),
				WithPAStatusService(testRevision), WithTargetAnnotation("1")),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady, WithNumActivators(0)),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: hpa(pa(testNamespace, testRevision, WithHPAClass, WithTargetAnnotation("1"), WithMetricAnnotation("cpu"))),
		}},
		WantErr: true,
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("update", "horizontalpodautoscalers"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update HPA: inducing failure for update horizontalpodautoscalers"),
		},
	}, {
		Name: "update hpa with target usage",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithPASKSReady,
				WithTraffic, WithScaleTargetInitialized, withScales(0, 0),
				WithPAStatusService(testRevision), WithPAMetricsService(privateSvc), WithTargetAnnotation("1")),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady, WithNumActivators(0)),
		},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: hpa(pa(testNamespace, testRevision, WithHPAClass, WithPASKSReady,
				WithTargetAnnotation("1"), WithMetricAnnotation("cpu"))),
		}},
	}, {
		Name: "invalid key",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass),
		},
		Key: "sandwich///",
	}, {
		Name: "failure to create HPA",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, withScales(0, 0), WithHPAClass),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WantCreates: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("create", "horizontalpodautoscalers"),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, withScales(0, 0),
				WithNoTraffic(
					"FailedCreate", `Failed to create HorizontalPodAutoscaler "test-revision".`)),
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create HPA: inducing failure for create horizontalpodautoscalers"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		ctx = podscalable.WithDuck(ctx)

		r := &Reconciler{
			Base: &areconciler.Base{
				Client:           servingclient.Get(ctx),
				NetworkingClient: networkingclient.Get(ctx),
				SKSLister:        listers.GetServerlessServiceLister(),
				MetricLister:     listers.GetMetricLister(),
			},
			kubeClient: kubeclient.Get(ctx),
			hpaLister:  listers.GetHorizontalPodAutoscalerLister(),
		}
		return pareconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetPodAutoscalerLister(), controller.GetEventRecorder(ctx), r, asconfig.HPA,
			controller.Options{
				ConfigStore: &testConfigStore{config: defaultConfig()},
			})
	}))
}

func sks(ns, n string, so ...SKSOption) *nv1a1.ServerlessService {
	hpa := pa(ns, n, WithHPAClass)
	s := aresources.MakeSKS(hpa, nv1a1.SKSOperationModeServe, 0)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

func pa(namespace, name string, options ...PodAutoscalerOption) *asv1a1.PodAutoscaler {
	pa := &asv1a1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: asv1a1.PodAutoscalerSpec{
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name + "-deployment",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
	}
	pa.Status.InitializeConditions()
	for _, opt := range options {
		opt(pa)
	}
	return pa
}

type hpaOption func(*autoscalingv2beta1.HorizontalPodAutoscaler)

func withHPAOwnersRemoved(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
	hpa.OwnerReferences = nil
}

func withScales(d, a int32) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(d), ptr.Int32(a)
	}
}
func withHPAScaleStatus(d, a int32) hpaOption {
	return func(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
		hpa.Status.DesiredReplicas, hpa.Status.CurrentReplicas = d, a
	}
}

func hpa(pa *asv1a1.PodAutoscaler, options ...hpaOption) *autoscalingv2beta1.HorizontalPodAutoscaler {
	h := resources.MakeHPA(pa, defaultConfig().Autoscaler)
	for _, o := range options {
		o(h)
	}
	return h
}

type deploymentOption func(*appsv1.Deployment)

func deploy(namespace, name string, opts ...deploymentOption) *appsv1.Deployment {
	s := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-deployment",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"a": "b",
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas: 42,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func defaultConfig() *config.Config {
	autoscalerConfig, _ := asconfig.NewConfigFromMap(nil)
	return &config.Config{
		Autoscaler: autoscalerConfig,
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ reconciler.ConfigStore = (*testConfigStore)(nil)
