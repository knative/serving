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
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/autoscaling/v2beta1/horizontalpodautoscaler/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	fakepainformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/autoscaling"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/networking"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/reconciler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
	aresources "knative.dev/serving/pkg/reconciler/autoscaling/resources"
	presources "knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
	. "knative.dev/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestControllerCanReconcile(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctl := NewController(ctx, configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: map[string]string{},
	}))

	podAutoscaler := pa(testNamespace, testRevision, WithHPAClass)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(podAutoscaler)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(podAutoscaler)

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
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic, WithPAStatusService(testRevision)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "metric-change",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency))),
			pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency),
				WithMSvcStatus(testRevision+"-metrics"), WithTraffic, WithPAStatusService(testRevision)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metric(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)), testRevision+"-metrics2"),
			metricsSvc(testNamespace, testRevision, WithSvcSelector(usualSelector),
				SvcWithAnnotationValue(autoscaling.ClassAnnotationKey, autoscaling.HPA),
				SvcWithAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency)),
		},
		Key:         key(testNamespace, testRevision),
		WantCreates: []runtime.Object{},
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: metric(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)), testRevision+"-metrics"),
		}},
	}, {
		Name: "create hpa & sks",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet")),
		}},
	}, {
		Name: "create hpa, sks and metric service when Concurrency used",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WantCreates: []runtime.Object{
			metric(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)), testRevision+"-metrics"),
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency))),
			metricsSvc(testNamespace, testRevision, WithSvcSelector(usualSelector),
				SvcWithAnnotationValue(autoscaling.ClassAnnotationKey, autoscaling.HPA),
				SvcWithAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency)),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency),
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithMSvcStatus(testRevision+"-metrics")),
		}},
	}, {
		Name: "create hpa, sks and metric service when RPS used",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.RPS)),
			deploy(testNamespace, testRevision),
		},
		Key: key(testNamespace, testRevision),
		WantCreates: []runtime.Object{
			metric(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.RPS)), testRevision+"-metrics"),
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
			hpa(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.RPS))),
			metricsSvc(testNamespace, testRevision, WithSvcSelector(usualSelector),
				SvcWithAnnotationValue(autoscaling.ClassAnnotationKey, autoscaling.HPA),
				SvcWithAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.RPS)),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.RPS),
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithMSvcStatus(testRevision+"-metrics")),
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
			Object: pa(testNamespace, testRevision, WithHPAClass, WithTraffic,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithPAStatusService(testRevision)),
		}},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "reconcile sks becomes ready",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass,
				WithTraffic, WithPAStatusService(testRevision)),
		}},
		Key: key(testNamespace, testRevision),
	}, {
		Name: "reconcile sks",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef("bar"),
				WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithTraffic, WithPAStatusService(testRevision)),
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
			Object: pa(testNamespace, testRevision, WithHPAClass,
				WithNoTraffic("ServicesNotReady", "SKS Services are not ready yet"),
				WithPAStatusService(testRevision)),
		}},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName),
				WithPubService, WithPrivateService),
		}},
	}, {
		Name: "reconcile sks - update fails",
		Objects: []runtime.Object{
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic),
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
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic),
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
		Name: "metric is disowned",
		Objects: []runtime.Object{
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency))),
			pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency),
				WithMSvcStatus(testRevision+"-metrics"), WithTraffic, WithPAStatusService(testRevision)),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metric(pa(testNamespace, testRevision,
				WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency)), testRevision+"-metrics", WithMetricOwnersRemoved),
			metricsSvc(testNamespace, testRevision, WithSvcSelector(usualSelector),
				SvcWithAnnotationValue(autoscaling.ClassAnnotationKey, autoscaling.HPA),
				SvcWithAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency)),
		},
		Key:     key(testNamespace, testRevision),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation(autoscaling.Concurrency),
				WithMSvcStatus(testRevision+"-metrics"), WithTraffic, WithPAStatusService(testRevision), MarkResourceNotOwnedByPA("Metric", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `error reconciling metric: PA: test-revision does not own Metric: test-revision`),
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
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			pa(testNamespace, testRevision, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: pa(testNamespace, testRevision, WithHPAClass,
				WithTraffic, WithPAStatusService(testRevision)),
		}},
		Key:     key(testNamespace, testRevision),
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
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic,
				WithPAStatusService(testRevision), WithTargetAnnotation("1")),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
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
			pa(testNamespace, testRevision, WithHPAClass, WithTraffic,
				WithPAStatusService(testRevision), WithTargetAnnotation("1")),
			hpa(pa(testNamespace, testRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			deploy(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(testNamespace, testRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: hpa(pa(testNamespace, testRevision, WithHPAClass, WithTargetAnnotation("1"), WithMetricAnnotation("cpu"))),
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
			pa(testNamespace, testRevision, WithHPAClass),
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
			Object: pa(testNamespace, testRevision, WithHPAClass, WithNoTraffic(
				"FailedCreate", `Failed to create HorizontalPodAutoscaler "test-revision".`)),
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create HPA: inducing failure for create horizontalpodautoscalers"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		psFactory := presources.NewPodScalableInformerFactory(ctx)

		return &Reconciler{
			Base: &areconciler.Base{
				Base:              reconciler.NewBase(ctx, controllerAgentName, cmw),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				MetricLister:      listers.GetMetricLister(),
				ConfigStore:       &testConfigStore{config: defaultConfig()},
				ServiceLister:     listers.GetK8sServiceLister(),
				PSInformerFactory: psFactory,
			},
			hpaLister: listers.GetHorizontalPodAutoscalerLister(),
		}
	}))
}

func sks(ns, n string, so ...SKSOption) *nv1a1.ServerlessService {
	hpa := pa(ns, n, WithHPAClass)
	s := aresources.MakeSKS(hpa, nv1a1.SKSOperationModeServe)
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
	for _, opt := range options {
		opt(pa)
	}
	return pa
}

type hpaOption func(*autoscalingv2beta1.HorizontalPodAutoscaler)

func withHPAOwnersRemoved(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
	hpa.OwnerReferences = nil
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

func metricsSvc(ns, n string, opts ...K8sServiceOption) *corev1.Service {
	pa := pa(ns, n)
	svc := aresources.MakeMetricsService(pa, map[string]string{})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

type metricOption func(*asv1a1.Metric)

func metric(pa *asv1a1.PodAutoscaler, msvcName string, opts ...metricOption) *asv1a1.Metric {
	m := aresources.MakeMetric(context.Background(), pa, msvcName, defaultConfig().Autoscaler)
	for _, o := range opts {
		o(m)
	}
	return m
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
