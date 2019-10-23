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

package kpa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	// These are the fake informers we want setup.
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	fakedynamicclient "knative.dev/pkg/injection/clients/dynamicclient/fake"
	"knative.dev/pkg/kmeta"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakemetricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	fakepainformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	fakesksinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/reconciler"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/apis/autoscaling"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa/resources"
	aresources "knative.dev/serving/pkg/reconciler/autoscaling/resources"
	revisionresources "knative.dev/serving/pkg/reconciler/revision/resources"
	presources "knative.dev/serving/pkg/resources"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
	. "knative.dev/serving/pkg/testing"
)

const (
	gracePeriod              = 60 * time.Second
	stableWindow             = 5 * time.Minute
	paStableWindow           = 45 * time.Second
	defaultConcurrencyTarget = 10.0
	defaultTU                = 0.5
)

func defaultConfigMapData() map[string]string {
	return map[string]string{
		"max-scale-up-rate":                       "12.0",
		"container-concurrency-target-percentage": fmt.Sprintf("%f", defaultTU),
		"container-concurrency-target-default":    fmt.Sprintf("%f", defaultConcurrencyTarget),
		"stable-window":                           stableWindow.String(),
		"panic-window":                            "10s",
		"scale-to-zero-grace-period":              gracePeriod.String(),
		"tick-interval":                           "2s",
	}
}

func defaultConfig() *config.Config {
	autoscalerConfig, _ := autoscaler.NewConfigFromMap(defaultConfigMapData())
	return &config.Config{
		Autoscaler: autoscalerConfig,
	}
}

func newConfigWatcher() configmap.Watcher {
	return configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      autoscaler.ConfigName,
		},
		Data: defaultConfigMapData(),
	})
}

func withScales(g, w int32) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(w), ptr.Int32(g)
	}
}

func withMSvcName(sn string) K8sServiceOption {
	return func(svc *corev1.Service) {
		svc.Name = sn
		svc.GenerateName = ""
	}
}

func metricsSvc(ns, n string, opts ...K8sServiceOption) *corev1.Service {
	pa := kpa(ns, n)
	svc := aresources.MakeMetricsService(pa, map[string]string{})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func metricWithDiffSvc(ns, n string) *asv1a1.Metric {
	m := metric(ns, n)
	m.Spec.ScrapeTarget = "something-else"
	return m
}

type metricOption func(*asv1a1.Metric)

func metric(ns, n string, opts ...metricOption) *asv1a1.Metric {
	pa := kpa(ns, n)
	m := aresources.MakeMetric(context.Background(), pa,
		kmeta.ChildName(n, "-metrics"), defaultConfig().Autoscaler)
	for _, o := range opts {
		o(m)
	}
	return m
}

func sks(ns, n string, so ...SKSOption) *nv1a1.ServerlessService {
	kpa := kpa(ns, n)
	s := aresources.MakeSKS(kpa, nv1a1.SKSOperationModeServe)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func markOld(pa *asv1a1.PodAutoscaler) {
	pa.Status.Conditions[0].LastTransitionTime.Inner.Time = time.Now().Add(-1 * time.Hour)
}

func withSvcSelector(sel map[string]string) K8sServiceOption {
	return func(s *corev1.Service) {
		s.Spec.Selector = sel
	}
}

func markActivating(pa *asv1a1.PodAutoscaler) {
	pa.Status.MarkActivating("Queued", "Requests to the target are being buffered as resources are provisioned.")
}

func markActive(pa *asv1a1.PodAutoscaler) {
	pa.Status.MarkActive()
}

func markUnknown(pa *asv1a1.PodAutoscaler) {
	pa.Status.MarkActivating("", "")
}

func markInactive(pa *asv1a1.PodAutoscaler) {
	pa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")
}

func withMSvcStatus(s string) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Status.MetricsServiceName = kmeta.ChildName(s, "-metrics")
	}
}

func kpa(ns, n string, opts ...PodAutoscalerOption) *asv1a1.PodAutoscaler {
	rev := newTestRevision(ns, n)
	kpa := revisionresources.MakePA(rev)
	kpa.Annotations["autoscaling.knative.dev/class"] = "kpa.autoscaling.knative.dev"
	kpa.Annotations["autoscaling.knative.dev/metric"] = "concurrency"
	for _, opt := range opts {
		opt(kpa)
	}
	return kpa
}

func markResourceNotOwned(rType, name string) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Status.MarkResourceNotOwned(rType, name)
	}
}

func TestReconcile(t *testing.T) {
	const (
		key          = testNamespace + "/" + testRevision
		deployName   = testRevision + "-deployment"
		defaultScale = 11
		unknownScale = scaleUnknown
		underscale   = defaultScale - 1
		overscale    = defaultScale + 1
	)
	usualSelector := map[string]string{"a": "b"}

	// Set up a default deployment with the appropriate scale so that we don't
	// see patches to correct that scale.
	defaultDeployment := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(defaultScale)
	})

	// Setup underscaled and overscsaled deployment
	underscaledDeployment := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(underscale)
	})
	overscaledDeployment := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(overscale)
	})

	minScalePatch := clientgotesting.PatchActionImpl{
		ActionImpl: clientgotesting.ActionImpl{Namespace: testNamespace},
		Name:       deployName,
		Patch:      []byte(fmt.Sprintf(`[{"op":"replace","path":"/spec/replicas","value":%d}]`, defaultScale)),
	}

	inactiveKPAMinScale := func(g int32) *asv1a1.PodAutoscaler {
		return kpa(
			testNamespace, testRevision, markInactive, withScales(g, unknownScale), WithReachabilityReachable,
			withMinScale(defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision),
		)
	}
	activatingKPAMinScale := func(g int32) *asv1a1.PodAutoscaler {
		return kpa(
			testNamespace, testRevision, markActivating, withScales(g, defaultScale), WithReachabilityReachable,
			withMinScale(defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision),
		)
	}
	activeKPAMinScale := func(g, w int32) *asv1a1.PodAutoscaler {
		return kpa(
			testNamespace, testRevision, markActive, withScales(g, w), WithReachabilityReachable,
			withMinScale(defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision),
		)
	}

	defaultSKS := sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady)
	defaultMetricsSvc := metricsSvc(
		testNamespace, testRevision, withSvcSelector(usualSelector))
	defaultMetric := metric(testNamespace, testRevision)

	underscaledEndpoints := makeSKSPrivateEndpoints(underscale, testNamespace, testRevision)
	overscaledEndpoints := makeSKSPrivateEndpoints(overscale, testNamespace, testRevision)
	defaultEndpoints := makeSKSPrivateEndpoints(1, testNamespace, testRevision)
	zeroEndpoints := makeSKSPrivateEndpoints(0, testNamespace, testRevision)

	deciderKey := struct{}{}

	// Note: due to how KPA reconciler works we are dependent on the
	// two constant objects above, which means, that all tests must share
	// the same namespace and revision name.
	table := TableTest{{
		Name:                    "bad workqueue key, Part I",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "bad workqueue key, Part II",
		Key:                     "too-few-parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name: "steady state",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
	}, {
		Name: "no endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMSvcStatus(testRevision), WithPAStatusService(testRevision),
				withScales(1, defaultScale)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markUnknown, withMSvcStatus(testRevision), withScales(1, defaultScale),
				WithPAStatusService(testRevision)),
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error checking endpoints test-revision-private: endpoints "test-revision-private" not found`),
		},
	}, {
		Name: "metric-service-mismatch",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision+"-other"),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector), withMSvcName("whatever")),
			metric(testNamespace, testRevision),
			defaultDeployment,
			defaultEndpoints,
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "core",
					Version:  "v1",
					Resource: "services",
				},
			},
			Name: "whatever",
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive,
				withScales(1, defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
		}},
		WantCreates: []runtime.Object{
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
	}, {
		Name: "failure-creating-metric-object",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			defaultDeployment, defaultEndpoints,
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "metrics"),
		},
		WantCreates: []runtime.Object{
			metric(testNamespace, testRevision),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling Metric: error creating metric: inducing failure for create metrics`),
		},
		WantErr: true,
	}, {
		Name: "failure-updating-metric-object",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			defaultDeployment, defaultEndpoints,
			metricWithDiffSvc(testNamespace, testRevision),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "metrics"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric(testNamespace, testRevision),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling Metric: error updating metric: inducing failure for update metrics`),
		},
		WantErr: true,
	}, {
		Name: "create metrics service",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			defaultDeployment,
			defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withScales(1, defaultScale),
				WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
		}},
		WantCreates: []runtime.Object{
			metric(testNamespace, testRevision),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
	}, {
		Name: "create metrics service failure",
		Key:  key,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			defaultSKS,
			defaultDeployment,
			defaultEndpoints,
		},
		WantCreates: []runtime.Object{
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling metrics Service: error creating metrics K8s service for test-namespace/test-revision: inducing failure for create services`),
		},
	}, {
		Name: "scale up deployment",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision),
			defaultEndpoints,
		},
		WantPatches: []clientgotesting.PatchActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
			},
			Name:  deployName,
			Patch: []byte(`[{"op":"add","path":"/spec/replicas","value":11}]`),
		}},
	}, {
		Name: "scale up deployment failure",
		Key:  key,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "deployments"),
		},
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision),
			defaultEndpoints,
		},
		WantPatches: []clientgotesting.PatchActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
			},
			Name:  deployName,
			Patch: []byte(`[{"op":"add","path":"/spec/replicas","value":11}]`),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error scaling target: failed to apply scale to scale target test-revision-deployment: inducing failure for patch deployments`),
		},
	}, {
		Name: "update metrics service",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			defaultDeployment, defaultEndpoints,
			metricsSvc(testNamespace, testRevision),
			metric(testNamespace, testRevision),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		}},
	}, {
		Name: "update metrics service fails",
		Key:  key,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			defaultDeployment, defaultEndpoints,
			metricsSvc(testNamespace, testRevision),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling metrics Service: error updating K8s Service test-revision-metrics: inducing failure for update services`),
		},
	}, {
		Name:    "metrics service isn't owned",
		Key:     key,
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				withScales(1, defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector), func(s *corev1.Service) {
				s.OwnerReferences = nil
			}),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, WithPAStatusService(testRevision),
				withMSvcStatus(testRevision), withScales(1, defaultScale),
				// We expect this change in status:
				markResourceNotOwned("Service", testRevision+"-metrics")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling metrics Service: PA: test-revision does not own Service: test-revision-metrics"),
		},
	}, {
		Name: "can't read endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error checking endpoints test-revision-private: endpoints "test-revision-private" not found`),
		},
	}, {
		Name: "pa activates",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				withScales(0, defaultScale), WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			// SKS is ready here, since its endpoints are populated with Activator endpoints.
			sks(testNamespace, testRevision, WithProxyMode, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
			// When PA is passive num private endpoints must be 0.
			zeroEndpoints,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName)),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, withScales(0, defaultScale),
				withMSvcStatus(testRevision),
				WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks is still not ready",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, WithTraffic, withMSvcStatus(testRevision),
				withScales(0, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithPubService,
				WithPrivateService),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, withScales(0, defaultScale),
				withMSvcStatus(testRevision), WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks becomes ready",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, WithPAStatusService(testRevision),
				withMSvcStatus(testRevision), withScales(1, defaultScale)),
		}},
	}, {
		Name: "kpa does not become ready without minScale endpoints when reachable",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMinScale(2), withScales(1, defaultScale),
				WithReachabilityReachable, withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, withMinScale(2), withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision), WithReachabilityReachable),
		}},
	}, {
		Name: "kpa does not become ready without minScale endpoints when reachability is unknown",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMinScale(2), withScales(1, defaultScale),
				withMSvcStatus(testRevision), WithReachabilityUnknown),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, withMinScale(2), withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision), WithReachabilityUnknown),
		}},
	}, {
		Name: "kpa becomes ready without minScale endpoints when unreachable",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMinScale(2), withScales(1, defaultScale),
				withMSvcStatus(testRevision), WithReachabilityUnreachable),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withMinScale(2), withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision), WithReachabilityUnreachable),
		}},
	}, {
		Name: "kpa becomes ready with minScale endpoints when reachable",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActivating, withMinScale(2), WithPAStatusService(testRevision),
				withMSvcStatus(testRevision), withScales(1, defaultScale), WithReachabilityReachable),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
			makeSKSPrivateEndpoints(2, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withMinScale(2), withMSvcStatus(testRevision),
				withScales(2, defaultScale), WithPAStatusService(testRevision), WithReachabilityReachable),
		}},
	}, {
		Name: "kpa becomes ready with minScale endpoints when reachability is unknown",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActivating, withMinScale(2), WithPAStatusService(testRevision),
				withMSvcStatus(testRevision), withScales(1, defaultScale), WithReachabilityUnknown),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
			makeSKSPrivateEndpoints(2, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withMinScale(2), withMSvcStatus(testRevision),
				withScales(2, defaultScale), WithPAStatusService(testRevision), WithReachabilityUnknown),
		}},
	}, {
		Name: "sks does not exist",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision), withScales(1, defaultScale)),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// SKS does not exist, so we're just creating and have no status.
			Object: kpa(testNamespace, testRevision, markActivating, withMSvcStatus(testRevision), withScales(0, defaultScale)),
		}},
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
		},
	}, {
		Name: "sks is out of whack",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(0, defaultScale), withMSvcStatus(testRevision), markActive),
			sks(testNamespace, testRevision, WithDeployRef("bar"),
				WithPubService),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// SKS just got updated and we don't have up to date status.
			Object: kpa(testNamespace, testRevision, markActivating, withMSvcStatus(testRevision),
				withScales(0, defaultScale), WithPAStatusService(testRevision)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithPubService,
				WithDeployRef(deployName)),
		}},
	}, {
		Name: "sks cannot be created",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision), withScales(1, defaultScale)),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "serverlessservices"),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"error reconciling SKS: error creating SKS test-revision: inducing failure for create serverlessservices"),
		},
	}, {
		Name: "sks cannot be updated",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(1, defaultScale), withMSvcStatus(testRevision), markActive),
			sks(testNamespace, testRevision, WithDeployRef("bar")),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: error updating SKS test-revision: inducing failure for update serverlessservices"),
		},
	}, {
		Name: "sks is disowned",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(1, defaultScale), withMSvcStatus(testRevision), markActive),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady,
				WithSKSOwnersRemoved),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment,
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, withScales(1, defaultScale),
				withMSvcStatus(testRevision), markResourceNotOwned("ServerlessService", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: PA: test-revision does not own SKS: test-revision"),
		},
	}, {
		Name: "metric is disowned",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(1, defaultScale), withMSvcStatus(testRevision), markActive),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, WithMetricOwnersRemoved),
			defaultDeployment,
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, withScales(1, defaultScale),
				withMSvcStatus(testRevision), markResourceNotOwned("Metric", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `error reconciling Metric: PA: test-revision does not own Metric: test-revision`),
		},
	}, {
		Name: "steady not serving",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 0 /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(0, 0),
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
				d.Spec.Replicas = ptr.Int32(0)
			}),
			// Should be present, but empty.
			zeroEndpoints,
		},
	}, {
		Name: "steady not serving (scale to zero)",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 0 /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withScales(0, 0),
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision),
			// Should be present, but empty.
			zeroEndpoints,
		},
		WantPatches: []clientgotesting.PatchActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
			},
			Name:  deployName,
			Patch: []byte(`[{"op":"add","path":"/spec/replicas","value":0}]`),
		}},
	}, {
		Name: "from serving to proxy",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0 /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, markOld, withScales(0, 0),
				WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision), defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, withScales(1, 0),
				withMSvcStatus(testRevision),
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
	}, {
		Name: "scaling to 0, but not stable for long enough, so no-op",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0 /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withScales(1, 1),
				WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision), defaultEndpoints,
		},
	}, {
		Name: "activation failure",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey, decider(testNamespace,
			testRevision, 0 /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActivating, markOld,
				WithPAStatusService(testRevision), withScales(0, 0),
				withMSvcStatus(testRevision)),
			defaultSKS,
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			deploy(testNamespace, testRevision), defaultEndpoints,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, withMSvcStatus(testRevision),
				WithNoTraffic("TimedOut", "The target could not be activated."), withScales(1, 0),
				WithPAStatusService(testRevision), withMSvcStatus(testRevision)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
			},
			Name:  deployName,
			Patch: []byte(`[{"op":"add","path":"/spec/replicas","value":0}]`),
		}},
	}, {
		Name: "want=-1, underscaled, PA inactive",
		// No-op
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, unknownScale, 0 /* ebc */)),
		Objects: []runtime.Object{
			inactiveKPAMinScale(0), underscaledEndpoints, underscaledDeployment,
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode,
				WithPubService, WithPrivateService),
			defaultMetric, defaultMetricsSvc,
		},
	}, {
		Name: "want=1, underscaled, PA inactive",
		// Status -> Activating and Deployment has to be patched.
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 1, 0 /* ebc */)),
		Objects: []runtime.Object{
			inactiveKPAMinScale(0), underscaledEndpoints, underscaledDeployment,
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode,
				WithPubService, WithPrivateService),
			defaultMetric, defaultMetricsSvc,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activatingKPAMinScale(0),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName),
				WithPubService, WithPrivateService),
		}},
	}, {
		Name: "underscaled, PA activating",
		// Scale to `minScale`
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 2 /*autoscaler desired scale*/, 0 /* ebc */)),
		Objects: []runtime.Object{
			activatingKPAMinScale(underscale), underscaledEndpoints, underscaledDeployment,
			defaultSKS, defaultMetric, defaultMetricsSvc,
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
	}, {
		Name: "underscaled, PA active",
		// Mark PA "activating"
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, defaultScale, 0 /* ebc */)),
		Objects: []runtime.Object{
			activeKPAMinScale(underscale, defaultScale), underscaledEndpoints, underscaledDeployment,
			defaultSKS, defaultMetric, defaultMetricsSvc,
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activatingKPAMinScale(underscale),
		}},
	}, {
		// Scale to `minScale` and mark PA "active"
		Name: "overscaled, PA inactive",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 0 /*wantScale*/, 0 /* ebc */)),
		Objects: []runtime.Object{
			inactiveKPAMinScale(overscale), overscaledEndpoints, overscaledDeployment,
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			defaultMetric, defaultMetricsSvc,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activeKPAMinScale(overscale, defaultScale),
		}},
	}, {
		Name: "overscaled, PA activating",
		// Scale to `minScale` and mark PA "active"
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 1 /*wantScale*/, 0 /* ebc */)),
		Objects: []runtime.Object{
			inactiveKPAMinScale(overscale), overscaledEndpoints, overscaledDeployment,
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			defaultMetric, defaultMetricsSvc,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activeKPAMinScale(overscale, defaultScale),
		}},
	}, {
		Name: "over maxScale for real, PA active",
		// No-op.
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, overscale, /*want more than minScale*/
				0 /* ebc */)),
		Objects: []runtime.Object{
			activeKPAMinScale(overscale, overscale), overscaledEndpoints, overscaledDeployment,
			defaultSKS, defaultMetric, defaultMetricsSvc,
		},
	}, {
		Name: "over maxScale, need to scale down, PA active",
		// No-op.
		Key: key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, 1, /*less than minScale*/
				0 /* ebc */)),
		Objects: []runtime.Object{
			activeKPAMinScale(overscale, overscale), overscaledEndpoints, overscaledDeployment,
			defaultSKS, defaultMetric, defaultMetricsSvc,
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activeKPAMinScale(overscale, defaultScale),
		}},
	}, {
		Name: "scaled-to-0-no-scale-data",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, unknownScale /* desiredScale */, 0 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markInactive, withMSvcStatus(testRevision),
				withScales(0, -1), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithPubService),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
	}, {
		Name: "steady not enough capacity",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, defaultScale /* desiredScale */, -42 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
	}, {
		Name: "traffic increased, no longer enough burst capacity",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, defaultScale /* desiredScale */, -18 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
	}, {
		Name: "traffic decreased, now we have enough burst capacity",
		Key:  key,
		Ctx: context.WithValue(context.Background(), deciderKey,
			decider(testNamespace, testRevision, defaultScale /* desiredScale */, 1 /* ebc */)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus(testRevision),
				withScales(1, defaultScale), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady, WithProxyMode),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision),
			defaultDeployment, defaultEndpoints,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName)),
		}},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		// TODO(vagababov): see if we can get rid of the static piece of configuration and
		// constant namespace and revision names.

		// Make new decider if it's not in the context
		if d := ctx.Value(deciderKey); d == nil {
			decider := resources.MakeDecider(
				ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "trying-hard-to-care-in-this-test")
			decider.Status.DesiredScale = defaultScale
			decider.Generation = 2112
			fakeDeciders.Create(ctx, decider)
		} else {
			fakeDeciders.Create(ctx, d.(*autoscaler.Decider))
		}

		psFactory := presources.NewPodScalableInformerFactory(ctx)
		scaler := newScaler(ctx, psFactory, func(interface{}, time.Duration) {})
		scaler.activatorProbe = func(*asv1a1.PodAutoscaler, http.RoundTripper) (bool, error) { return true, nil }
		return &Reconciler{
			Base: &areconciler.Base{
				Base:              reconciler.NewBase(ctx, controllerAgentName, newConfigWatcher()),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				ServiceLister:     listers.GetK8sServiceLister(),
				MetricLister:      listers.GetMetricLister(),
				ConfigStore:       &testConfigStore{config: defaultConfig()},
				PSInformerFactory: psFactory,
			},
			endpointsLister: listers.GetEndpointsLister(),
			deciders:        fakeDeciders,
			scaler:          scaler,
		}
	}))
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

func TestGlobalResyncOnUpdateAutoscalerConfigMap(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	watcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	fakeDeciders := newTestDeciders()
	ctl := NewController(ctx, watcher, fakeDeciders)

	// Load default config
	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscaler.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: defaultConfigMapData(),
	})

	grp := errgroup.Group{}
	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatalf("failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatalf("failed to start configmap watcher: %v", err)
	}

	grp.Go(func() error { controller.StartAll(ctx.Done(), ctl); return nil })

	rev := newTestRevision(testNamespace, testRevision)
	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	kpa := revisionresources.MakePA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	// Wait for decider to be created.
	if decider, err := pollDeciders(fakeDeciders, testNamespace, testRevision, nil); err != nil {
		t.Fatalf("Failed to get decider: %v", err)
	} else if got, want := decider.Spec.TargetValue, defaultConcurrencyTarget*defaultTU; got != want {
		t.Fatalf("TargetValue = %v, want %v", got, want)
	}

	concurrencyTargetAfterUpdate := 100.0
	data := defaultConfigMapData()
	data["container-concurrency-target-default"] = fmt.Sprintf("%v", concurrencyTargetAfterUpdate)
	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscaler.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: data,
	})

	// Wait for decider to be updated with the new values from the configMap.
	cond := func(d *autoscaler.Decider) bool {
		return d.Spec.TargetValue == concurrencyTargetAfterUpdate
	}
	if decider, err := pollDeciders(fakeDeciders, testNamespace, testRevision, cond); err != nil {
		t.Fatalf("Failed to get decider: %v", err)
	} else if got, want := decider.Spec.TargetValue, concurrencyTargetAfterUpdate*defaultTU; got != want {
		t.Fatalf("TargetValue = %v, want %v", got, want)
	}
}

func TestControllerSynchronizesCreatesAndDeletes(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	defer cancel()

	fakeDeciders := newTestDeciders()
	ctl := NewController(ctx, newConfigWatcher(), fakeDeciders)

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	ep := makeSKSPrivateEndpoints(1, testNamespace, testRevision)
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
	fakeendpointsinformer.Get(ctx).Informer().GetIndexer().Add(ep)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	kpa := revisionresources.MakePA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	sks := sks(testNamespace, testRevision, WithDeployRef(kpa.Spec.ScaleTargetRef.Name),
		WithSKSReady)
	fakeservingclient.Get(ctx).NetworkingV1alpha1().ServerlessServices(testNamespace).Create(sks)
	fakesksinformer.Get(ctx).Informer().GetIndexer().Add(sks)

	msvc := aresources.MakeMetricsService(kpa, map[string]string{
		serving.RevisionLabelKey: rev.Name,
	})
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(msvc)
	fakeserviceinformer.Get(ctx).Informer().GetIndexer().Add(msvc)

	// Wait for the Reconcile to complete.
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if count := fakeDeciders.createCallCount.Load(); count != 1 {
		t.Fatalf("Create called %d times instead of once", count)
	}

	newKPA, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if !newKPA.Status.IsReady() {
		t.Error("Status.IsReady() was false")
	}

	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Delete(testRevision, nil)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Delete(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Delete(testRevision, nil)
	fakepainformer.Get(ctx).Informer().GetIndexer().Delete(kpa)
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if fakeDeciders.deleteCallCount.Load() == 0 {
		t.Fatal("Decider was not deleted")
	}

	if fakeDeciders.deleteBeforeCreate.Load() {
		t.Fatal("Deciders.Delete ran before OnPresent")
	}
}

func TestUpdate(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	defer cancel()

	fakeDeciders := newTestDeciders()
	ctl := NewController(ctx, newConfigWatcher(), fakeDeciders)

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	ep := makeSKSPrivateEndpoints(1, testNamespace, testRevision)
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
	fakeendpointsinformer.Get(ctx).Informer().GetIndexer().Add(ep)

	kpa := revisionresources.MakePA(rev)
	kpa.SetDefaults(context.Background())
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	msvc := aresources.MakeMetricsService(kpa, map[string]string{
		serving.RevisionLabelKey: rev.Name,
	})
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(msvc)
	fakeserviceinformer.Get(ctx).Informer().GetIndexer().Add(msvc)

	metric := aresources.MakeMetric(ctx, kpa, "", defaultConfig().Autoscaler)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().Metrics(testNamespace).Create(metric)
	fakemetricinformer.Get(ctx).Informer().GetIndexer().Add(metric)

	sks := sks(testNamespace, testRevision, WithDeployRef(kpa.Spec.ScaleTargetRef.Name),
		WithSKSReady)
	fakeservingclient.Get(ctx).NetworkingV1alpha1().ServerlessServices(testNamespace).Create(sks)
	fakesksinformer.Get(ctx).Informer().GetIndexer().Add(sks)

	decider := resources.MakeDecider(context.Background(), kpa, defaultConfig().Autoscaler, msvc.Name)

	// Wait for the Reconcile to complete.
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if count := fakeDeciders.createCallCount.Load(); count != 1 {
		t.Fatalf("Deciders.Create called %d times instead of once", count)
	}

	// Verify decider shape.
	if got, want := fakeDeciders.decider, decider; !cmp.Equal(got, want) {
		t.Errorf("decider mismatch: diff(+got, -want): %s", cmp.Diff(got, want))
	}

	newKPA, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "True" {
		t.Errorf("GetCondition(Ready) = %v, wanted True", cond)
	}

	// Update the KPA container concurrency.
	kpa.Spec.ContainerConcurrency = 2
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Update(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Update(kpa)

	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if fakeDeciders.updateCallCount.Load() == 0 {
		t.Fatal("Deciders.Update was not called")
	}
}

func TestControllerCreateError(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	defer cancel()

	key := testNamespace + "/" + testRevision
	want := apierrors.NewBadRequest("asdf")

	ctl := NewController(ctx, newConfigWatcher(),
		&failingDeciders{
			getErr:    apierrors.NewNotFound(asv1a1.Resource("Deciders"), key),
			createErr: want,
		})

	kpa := revisionresources.MakePA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	got := ctl.Reconciler.Reconcile(context.Background(), key)
	if !errors.Is(got, want) {
		t.Errorf("Reconcile() = %v, wanted %v wrapped", got, want)
	}
}

func TestControllerUpdateError(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	defer cancel()

	key := testNamespace + "/" + testRevision
	want := apierrors.NewBadRequest("asdf")

	ctl := NewController(ctx, newConfigWatcher(),
		&failingDeciders{
			getErr:    apierrors.NewNotFound(asv1a1.Resource("Deciders"), key),
			createErr: want,
		})

	kpa := revisionresources.MakePA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	got := ctl.Reconciler.Reconcile(context.Background(), key)
	if !errors.Is(got, want) {
		t.Errorf("Reconcile() = %v, wanted %v wrapped", got, want)
	}
}

func TestControllerGetError(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	key := testNamespace + "/" + testRevision
	want := apierrors.NewBadRequest("asdf")

	ctl := NewController(ctx, newConfigWatcher(),
		&failingDeciders{
			getErr: want,
		})

	kpa := revisionresources.MakePA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	got := ctl.Reconciler.Reconcile(context.Background(), key)
	if !errors.Is(got, want) {
		t.Errorf("Reconcile() = %v, wanted %v wrapped", got, want)
	}
}

func TestScaleFailure(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	ctl := NewController(ctx, newConfigWatcher(), newTestDeciders())

	// Only put the KPA in the lister, which will prompt failures scaling it.
	rev := newTestRevision(testNamespace, testRevision)
	kpa := revisionresources.MakePA(rev)
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err == nil {
		t.Error("Reconcile() = nil, wanted error")
	}
}

func pollDeciders(deciders *testDeciders, namespace, name string, cond func(*autoscaler.Decider) bool) (decider *autoscaler.Decider, err error) {
	wait.PollImmediate(10*time.Millisecond, 3*time.Second, func() (bool, error) {
		decider, err = deciders.Get(context.Background(), namespace, name)
		if err != nil {
			return false, nil
		}
		if cond == nil {
			return true, nil
		}
		return cond(decider), nil
	})
	return decider, err
}

func newTestDeciders() *testDeciders {
	return &testDeciders{
		createCallCount:    atomic.NewUint32(0),
		deleteCallCount:    atomic.NewUint32(0),
		updateCallCount:    atomic.NewUint32(0),
		deleteBeforeCreate: atomic.NewBool(false),
	}
}

type testDeciders struct {
	createCallCount    *atomic.Uint32
	deleteCallCount    *atomic.Uint32
	updateCallCount    *atomic.Uint32
	deleteBeforeCreate *atomic.Bool
	decider            *autoscaler.Decider
	mutex              sync.Mutex
}

func (km *testDeciders) Get(ctx context.Context, namespace, name string) (*autoscaler.Decider, error) {
	km.mutex.Lock()
	defer km.mutex.Unlock()

	if km.decider == nil {
		return nil, apierrors.NewNotFound(asv1a1.Resource("Deciders"), types.NamespacedName{Namespace: namespace, Name: name}.String())
	}
	return km.decider, nil
}

func (km *testDeciders) Create(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error) {
	km.mutex.Lock()
	defer km.mutex.Unlock()

	km.decider = decider
	km.createCallCount.Add(1)
	return decider, nil
}

func (km *testDeciders) Delete(ctx context.Context, namespace, name string) error {
	km.mutex.Lock()
	defer km.mutex.Unlock()

	km.decider = nil
	km.deleteCallCount.Add(1)
	if km.createCallCount.Load() == 0 {
		km.deleteBeforeCreate.Store(true)
	}
	return nil
}

func (km *testDeciders) Update(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error) {
	km.mutex.Lock()
	defer km.mutex.Unlock()

	km.decider = decider
	km.updateCallCount.Add(1)
	return decider, nil
}

func (km *testDeciders) Watch(fn func(types.NamespacedName)) {}

type failingDeciders struct {
	getErr    error
	createErr error
	deleteErr error
}

func (km *failingDeciders) Get(ctx context.Context, namespace, name string) (*autoscaler.Decider, error) {
	return nil, km.getErr
}

func (km *failingDeciders) Create(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error) {
	return nil, km.createErr
}

func (km *failingDeciders) Delete(ctx context.Context, namespace, name string) error {
	return km.deleteErr
}

func (km *failingDeciders) Watch(fn func(types.NamespacedName)) {
}

func (km *failingDeciders) Update(ctx context.Context, decider *autoscaler.Decider) (*autoscaler.Decider, error) {
	return decider, nil
}

func newTestRevision(namespace string, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/%s/revisions/%s", namespace, name),
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.KPA,
			},
		},
		Spec: v1alpha1.RevisionSpec{},
	}
}

func makeSKSPrivateEndpoints(num int, ns, n string) *corev1.Endpoints {
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      n + "-private",
		},
	}
	for i := 0; i < num; i++ {
		eps = addEndpoint(eps)
	}
	return eps
}

func addEndpoint(ep *corev1.Endpoints) *corev1.Endpoints {
	if ep.Subsets == nil {
		ep.Subsets = []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{},
		}}
	}

	ep.Subsets[0].Addresses = append(ep.Subsets[0].Addresses, corev1.EndpointAddress{IP: "127.0.0.1"})
	return ep
}

func withMinScale(minScale int) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Annotations = presources.UnionMaps(
			pa.Annotations,
			map[string]string{autoscaling.MinScaleAnnotationKey: strconv.Itoa(minScale)},
		)
	}
}

func decider(ns, name string, desiredScale, ebc int32) *autoscaler.Decider {
	return &autoscaler.Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.KPA,
			},
		},
		Spec: autoscaler.DeciderSpec{
			MaxScaleUpRate:      10.0,
			TickInterval:        2 * time.Second,
			TargetValue:         100,
			TotalValue:          100,
			TargetBurstCapacity: 211,
			PanicThreshold:      200,
			StableWindow:        60 * time.Second,
		},
		Status: autoscaler.DeciderStatus{
			DesiredScale:        desiredScale,
			ExcessBurstCapacity: ebc,
		},
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ reconciler.ConfigStore = (*testConfigStore)(nil)
