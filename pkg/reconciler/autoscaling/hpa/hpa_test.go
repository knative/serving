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
	"testing"

	// Inject our fake informers
	fakekubeclient "github.com/knative/pkg/injection/clients/kubeclient/fake"
	_ "github.com/knative/pkg/injection/informers/kubeinformers/autoscalingv2beta1/hpa/fake"
	_ "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service/fake"
	fakeservingclient "github.com/knative/serving/pkg/client/injection/client/fake"
	fakekpainformer "github.com/knative/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	_ "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/autoscaling"
	asv1a1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	autoscalingv1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/networking"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler"
	areconciler "github.com/knative/serving/pkg/reconciler/autoscaling"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"github.com/knative/serving/pkg/reconciler/autoscaling/hpa/resources"
	aresources "github.com/knative/serving/pkg/reconciler/autoscaling/resources"
	presources "github.com/knative/serving/pkg/resources"
	"go.uber.org/atomic"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	. "github.com/knative/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/testing/v1alpha1"
	. "github.com/knative/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestControllerCanReconcile(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	psFactory := presources.NewPodScalableInformerFactory(ctx)
	metrics := newTestMetrics()

	ctl := NewController(ctx, configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: map[string]string{},
	}), metrics, psFactory)

	podAutoscaler := pa(testRevision, testNamespace, WithHPAClass)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(podAutoscaler)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(podAutoscaler)

	err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision)
	if err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	_, err = fakekubeclient.Get(ctx).AutoscalingV2beta1().HorizontalPodAutoscalers(testNamespace).Get(testRevision, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting hpa: %v", err)
	}
}

func TestReconcile(t *testing.T) {
	const deployName = testRevision + "-deployment"
	usualSelector := map[string]string{"a": "b"}

	table := TableTest{{
		Name: "no op",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic, WithPAStatusService(testRevision)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(testRevision, testNamespace),
	}, {
		Name: "create hpa & sks",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass),
			deploy(testNamespace, testRevision),
		},
		Key: key(testRevision, testNamespace),
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace,
				WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet")),
		}},
	}, {
		Name: "create hpa, sks and metric service",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)),
			deploy(testNamespace, testRevision),
		},
		Key: key(testRevision, testNamespace),
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency))),
			metricsSvc(testNamespace, testRevision, WithSvcSelector(usualSelector),
				SvcWithAnnotationValue(autoscaling.ClassAnnotationKey, autoscaling.HPA),
				SvcWithAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency)),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency),
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithMSvcStatus(testRevision+"-00001")),
		}},
	}, {
		Name: "reconcile sks is still not ready",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithPubService,
				WithPrivateService(testRevision+"-rand")),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, WithTraffic,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithPAStatusService(testRevision)),
		}},
		Key: key(testRevision, testNamespace),
	}, {
		Name: "reconcile sks becomes ready",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass,
				WithTraffic, WithPAStatusService(testRevision)),
		}},
		Key: key(testRevision, testNamespace),
	}, {
		Name: "reconcile sks",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef("bar"),
				WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, WithTraffic, WithPAStatusService(testRevision)),
		}},
		Key: key(testRevision, testNamespace),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		}},
	}, {
		Name: "reconcile unhappy sks",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName+"-hairy"),
				WithPubService, WithPrivateService(testRevision+"-rand")),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithPAStatusService(testRevision)),
		}},
		Key: key(testRevision, testNamespace),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithPubService, WithPrivateService(testRevision+"-rand")),
		}},
	}, {
		Name: "reconcile sks - update fails",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef("bar"), WithSKSReady),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(testRevision, testNamespace),
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
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(testRevision, testNamespace),
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
			pa(testRevision, testNamespace, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSOwnersRemoved, WithSKSReady),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key:     key(testRevision, testNamespace),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, MarkResourceNotOwnedByPA("ServerlessService", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `error reconciling SKS: PA: test-revision does not own SKS: test-revision`),
		},
	}, {
		Name: "pa is disowned",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"), WithPAOwnersRemoved),
				withHPAOwnersRemoved),
		},
		Key:     key(testRevision, testNamespace),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, MarkResourceNotOwnedByPA("HorizontalPodAutoscaler", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`PodAutoscaler: "test-revision" does not own HPA: "test-revision"`),
		},
	}, {
		Name: "do not create hpa when non-hpa-class pod autoscaler",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithKPAClass),
		},
		Key: key(testRevision, testNamespace),
	}, {
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithPADeletionTimestamp),
			deploy(testNamespace, testRevision),
		},
		Key: key(testRevision, testNamespace),
	}, {
		Name: "update pa fails",
		Objects: []runtime.Object{
			hpa(testRevision, testNamespace,
				pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testRevision, testNamespace, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass,
				WithTraffic, WithPAStatusService(testRevision)),
		}},
		Key:     key(testRevision, testNamespace),
		WantErr: true,
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("update", "podautoscalers"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for PA "test-revision": inducing failure for update podautoscalers`),
		},
	}, {
		Name: "update hpa fails",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic,
				WithPAStatusService(testRevision), WithTargetAnnotation("1")),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			deploy(testNamespace, testRevision),
		},
		Key: key(testRevision, testNamespace),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithTargetAnnotation("1"), WithMetricAnnotation("cpu"))),
		}},
		WantErr: true,
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("update", "horizontalpodautoscalers"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for update horizontalpodautoscalers"),
		},
	}, {
		Name: "update hpa with target usage",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic,
				WithPAStatusService(testRevision), WithTargetAnnotation("1")),
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(testRevision, testNamespace),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithTargetAnnotation("1"), WithMetricAnnotation("cpu"))),
		}},
	}, {
		Name: "invalid key",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass),
		},
		Key: "sandwich///",
	}, {
		Name: "failure to create hpa",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass),
			deploy(testNamespace, testRevision),
		},
		Key: key(testRevision, testNamespace),
		WantCreates: []runtime.Object{
			hpa(testRevision, testNamespace, pa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		WithReactors: []ktesting.ReactionFunc{
			InduceFailure("create", "horizontalpodautoscalers"),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, WithNoTraffic(
				"FailedCreate", "Failed to create HorizontalPodAutoscaler \"test-revision\".")),
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for create horizontalpodautoscalers"),
		},
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		psFactory := presources.NewPodScalableInformerFactory(ctx)
		fakeMetrics := newTestMetrics()

		return &Reconciler{
			Base: &areconciler.Base{
				Base:              reconciler.NewBase(ctx, controllerAgentName, cmw),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				ConfigStore:       &testConfigStore{config: defaultConfig()},
				ServiceLister:     listers.GetK8sServiceLister(),
				PSInformerFactory: psFactory,
				Metrics:           fakeMetrics,
			},
			hpaLister: listers.GetHorizontalPodAutoscalerLister(),
		}
	}))
}

func sks(ns, n string, so ...SKSOption) *nv1a1.ServerlessService {
	hpa := pa(n, ns, WithHPAClass)
	s := aresources.MakeSKS(hpa, nv1a1.SKSOperationModeServe)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func key(name, namespace string) string {
	return namespace + "/" + name
}

func pa(name, namespace string, options ...PodAutoscalerOption) *autoscalingv1alpha1.PodAutoscaler {
	pa := &autoscalingv1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: autoscalingv1alpha1.PodAutoscalerSpec{
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name + "-deployment",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
	}
	for _, opt := range options {
		opt(pa)
	}
	return pa
}

type hpaOption func(*autoscalingv2beta1.HorizontalPodAutoscaler)

func withHPAOwnersRemoved(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
	hpa.OwnerReferences = nil
}

func hpa(name, namespace string, pa *autoscalingv1alpha1.PodAutoscaler, options ...hpaOption) *autoscalingv2beta1.HorizontalPodAutoscaler {
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

func metricsSvc(ns, n string, opts ...K8sServiceOption) *corev1.Service {
	pa := pa(n, ns)
	svc := aresources.MakeMetricsService(pa, map[string]string{})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func newTestMetrics() *testMetrics {
	return &testMetrics{
		createCallCount:    atomic.NewUint32(0),
		deleteCallCount:    atomic.NewUint32(0),
		updateCallCount:    atomic.NewUint32(0),
		deleteBeforeCreate: atomic.NewBool(false),
	}
}

type testMetrics struct {
	createCallCount    *atomic.Uint32
	deleteCallCount    *atomic.Uint32
	updateCallCount    *atomic.Uint32
	deleteBeforeCreate *atomic.Bool
	metric             *autoscaler.Metric
}

func (km *testMetrics) Get(ctx context.Context, namespace, name string) (*autoscaler.Metric, error) {
	if km.metric == nil {
		return nil, apierrors.NewNotFound(asv1a1.Resource("Metric"), autoscaler.NewMetricKey(namespace, name))
	}
	return km.metric, nil
}

func (km *testMetrics) Create(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	km.metric = metric
	km.createCallCount.Add(1)
	return metric, nil
}

func (km *testMetrics) Delete(ctx context.Context, namespace, name string) error {
	km.metric = nil
	km.deleteCallCount.Add(1)
	if km.createCallCount.Load() == 0 {
		km.deleteBeforeCreate.Store(true)
	}
	return nil
}

func (km *testMetrics) Update(ctx context.Context, metric *autoscaler.Metric) (*autoscaler.Metric, error) {
	km.metric = metric
	km.updateCallCount.Add(1)
	return metric, nil
}

func defaultConfig() *config.Config {
	autoscalerConfig, _ := autoscaler.NewConfigFromMap(nil)
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
