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
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	// These are the fake informers we want setup.
	fakedynamicclient "knative.dev/pkg/injection/clients/dynamicclient/fake"
	fakekubeclient "knative.dev/pkg/injection/clients/kubeclient/fake"
	fakeendpointsinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/service/fake"
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

	perrors "github.com/pkg/errors"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
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

// TODO(#3591): Convert KPA tests to table tests.

const (
	gracePeriod              = 60 * time.Second
	stableWindow             = 5 * time.Minute
	paStableWindow           = 45 * time.Second
	defaultConcurrencyTarget = 10.0
	defaultTU                = 0.5
)

func defaultConfigMapData() map[string]string {
	return map[string]string{
		"max-scale-up-rate":                       "1.0",
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

func metric(ns, n, msvcName string) *asv1a1.Metric {
	pa := kpa(ns, n)
	return aresources.MakeMetric(context.Background(), pa, msvcName, defaultConfig().Autoscaler)
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
		pa.Status.MetricsServiceName = s
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

// TestReconcileScaleUnknown tests the behaviour of the KPA when the Decider returns `scaleUnknown`
func TestReconcileScaleUnknown(t *testing.T) {
	const key = testNamespace + "/" + testRevision
	const deployName = testRevision + "-deployment"

	usualSelector := map[string]string{"a": "b"}

	desiredScale := int32(scaleUnknown)

	minScale := int32(4)
	underscale := minScale - 1
	overscale := minScale + 1

	underscaledDeployment := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(underscale)
	})
	overscaledDeployment := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(overscale)
	})

	minScalePatch := clientgotesting.PatchActionImpl{
		ActionImpl: clientgotesting.ActionImpl{Namespace: testNamespace},
		Name:       deployName,
		Patch:      []byte(fmt.Sprintf(`[{"op":"replace","path":"/spec/replicas","value":%d}]`, minScale)),
	}

	inactiveKpa := kpa(
		testNamespace, testRevision, markInactive,
		withMinScale(int(minScale)), WithPAStatusService(testRevision), withMSvcStatus("cargo"),
	)
	activatingKpa := kpa(
		testNamespace, testRevision, markActivating,
		withMinScale(int(minScale)), WithPAStatusService(testRevision), withMSvcStatus("cargo"),
	)
	activeKpa := kpa(
		testNamespace, testRevision, markActive,
		withMinScale(int(minScale)), WithPAStatusService(testRevision), withMSvcStatus("cargo"),
	)

	defaultSks := sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady)
	defaultMetricsSvc := metricsSvc(
		testNamespace, testRevision, withSvcSelector(usualSelector), withMSvcName("cargo"),
	)
	defaultMetric := metric(testNamespace, testRevision, "cargo")

	underscaledEndpoints := makeSKSPrivateEndpoints(int(underscale), testNamespace, testRevision)
	overscaledEndpoints := makeSKSPrivateEndpoints(int(overscale), testNamespace, testRevision)

	table := TableTest{{
		Name: "underscaled, PA inactive",
		// No-op
		Key: key,
		Objects: []runtime.Object{
			inactiveKpa, underscaledEndpoints, underscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}, {
		Name: "underscaled, PA activating",
		// Scale to `minScale`
		Key: key,
		Objects: []runtime.Object{
			activatingKpa, underscaledEndpoints, underscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}, {
		Name: "underscaled, PA active",
		// Mark PA "activating"
		Key: key,
		Objects: []runtime.Object{
			activeKpa, underscaledEndpoints, underscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activatingKpa,
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}, {
		Name: "overscaled, PA inactive",
		// No-op
		Key: key,
		Objects: []runtime.Object{
			inactiveKpa, overscaledEndpoints, overscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}, {
		Name: "overscaled, PA activating",
		// Scale to `minScale` and mark PA "active"
		Key: key,
		Objects: []runtime.Object{
			activatingKpa, overscaledEndpoints, overscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			minScalePatch,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: activeKpa,
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}, {
		Name: "overscaled, PA active",
		// No-op
		Key: key,
		Objects: []runtime.Object{
			activeKpa, overscaledEndpoints, overscaledDeployment,
			defaultSks, defaultMetric, defaultMetricsSvc,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: defaultMetric,
		}},
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		decider := resources.MakeDecider(ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "cheezburger")
		decider.Status.DesiredScale = desiredScale
		fakeDeciders.Create(ctx, decider)

		psFactory := presources.NewPodScalableInformerFactory(ctx)
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
			scaler:          newScaler(ctx, psFactory, func(interface{}, time.Duration) {}),
		}
	}))
}

func TestReconcileNegativeBurstCapacity(t *testing.T) {
	// This suite plays with different values for the excess burst capacity
	// and checks that SKS gets reconciled correctly inside KPA.
	const (
		key          = testNamespace + "/" + testRevision
		deployName   = testRevision + "-deployment"
		desiredScale = int32(5)
	)
	ebcKey := struct{}{}

	usualSelector := map[string]string{"a": "b"}

	expectedDeploy := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = ptr.Int32(desiredScale)
	})

	table := TableTest{{
		Name: "steady not enough capacity",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), ebcKey, int32(-1)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("yak-40"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("yak-40")),
			metric(testNamespace, testRevision, "yak-40"),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
			expectedDeploy,
		},
	}, {
		Name: "traffic increased, no longer enough burst capacity",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), ebcKey, int32(-1)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("yak-42"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("yak-42")),
			metric(testNamespace, testRevision, "yak-42"),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
			expectedDeploy,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
	}, {
		Name: "traffic decreased, now we have enough burst capacity",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), ebcKey, int32(1)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("ssj-100"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady, WithProxyMode),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("ssj-100")),
			metric(testNamespace, testRevision, "ssj-100"),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
			expectedDeploy,
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName)),
		}},
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		// Make sure we want to scale to 0.
		decider := resources.MakeDecider(
			ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "quite-important-here")
		decider.Status.DesiredScale = desiredScale
		decider.Status.ExcessBurstCapacity = ctx.Value(ebcKey).(int32)
		fakeDeciders.Create(ctx, decider)

		psFactory := presources.NewPodScalableInformerFactory(ctx)
		scaler := newScaler(ctx, psFactory, func(interface{}, time.Duration) {})
		scaler.activatorProbe = func(*asv1a1.PodAutoscaler, http.RoundTripper) (bool, error) { return true, nil }
		return &Reconciler{
			Base: &areconciler.Base{
				Base:              reconciler.NewBase(ctx, controllerAgentName, newConfigWatcher()),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				MetricLister:      listers.GetMetricLister(),
				ServiceLister:     listers.GetK8sServiceLister(),
				ConfigStore:       &testConfigStore{config: defaultConfig()},
				PSInformerFactory: psFactory,
			},
			endpointsLister: listers.GetEndpointsLister(),
			deciders:        fakeDeciders,
			scaler:          scaler,
		}
	}))
}

func TestReconcile(t *testing.T) {
	const key = testNamespace + "/" + testRevision
	const deployName = testRevision + "-deployment"
	usualSelector := map[string]string{"a": "b"}

	// Set up the deployment with the appropriate scale so that we don't
	// see patches to correct that scale.
	desiredScale := int32(11)
	expectedDeploy := deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
		d.Spec.Replicas = &desiredScale
	})

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
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("a330-200"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("a330-200")),
			metric(testNamespace, testRevision, "a330-200"),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
	}, {
		Name: "no endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMSvcStatus("a330-200x"), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("a330-200x")),
			metric(testNamespace, testRevision, "a330-200x"),
			expectedDeploy,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markUnknown, withMSvcStatus("a330-200x"), WithPAStatusService(testRevision)),
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error checking endpoints test-revision-rand: endpoints "test-revision-rand" not found`),
		},
	}, {
		Name: "metric-service-mismatch",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("a350-900ULR"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("b777-200LR")),
			metric(testNamespace, testRevision, "a350-900ULR"),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
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
			Name: "b777-200LR",
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric(testNamespace, testRevision, testRevision+"-00001"),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus(testRevision+"-00001")),
		}},
		WantCreates: []runtime.Object{
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
	}, {
		Name: "delete redundant metrics svc",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("a380-800"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("a380-800")),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("a380-900")),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("il96-300"), func(s *corev1.Service) {
					s.OwnerReferences = nil // This won't be removed, since we don't own it.
				}),
			metric(testNamespace, testRevision, "a380-800"),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
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
			Name: "a380-900",
		}},
	}, {
		Name: "make metrics service",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus(testRevision+"-00001")),
		}},
		WantCreates: []runtime.Object{
			metric(testNamespace, testRevision, testRevision+"-00001"),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
	}, {
		Name: "make metrics service failure",
		Key:  key,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantCreates: []runtime.Object{
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling metrics service: error creating metrics K8s service for test-namespace/test-revision: inducing failure for create services`),
		},
	}, {
		Name: "scale up deployment",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
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
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
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
				`error scaling target: inducing failure for patch deployments`),
		},
	}, {
		Name: "update metrics service",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("a321neo"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
			metricsSvc(testNamespace, testRevision, withMSvcName("a321neo")),
			metric(testNamespace, testRevision, "a321neo"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("a321neo")),
		}},
	}, {
		Name: "update metrics service fails",
		Key:  key,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("b767-300er"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
			metricsSvc(testNamespace, testRevision, withMSvcName("b767-300er")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("b767-300er")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error reconciling metrics service: error updating K8s Service b767-300er: inducing failure for update services`),
		},
	}, {
		Name:    "metrics service isn't owned",
		Key:     key,
		WantErr: true,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus("b737max-800")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector), func(s *corev1.Service) {
				s.OwnerReferences = nil
			}, withMSvcName("b737max-800")),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, WithPAStatusService(testRevision),
				withMSvcStatus("b737max-800"),
				// We expect this change in status:
				markResourceNotOwned("Service", "b737max-800")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling metrics service: PA: test-revision does not own Service: b737max-800"),
		},
	}, {
		Name: "can't read endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
		},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`error checking endpoints test-revision-rand: endpoints "test-revision-rand" not found`),
		},
	}, {
		Name: "pa activates",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				WithPAStatusService(testRevision)),
			// SKS is ready here, since its endpoints are populated with Activator endpoints.
			sks(testNamespace, testRevision, WithProxyMode, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			// When PA is passive num private endpoints must be 0.
			makeSKSPrivateEndpoints(0, testNamespace, testRevision),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName)),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks is still not ready",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, WithTraffic,
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithPubService, WithPrivateService(testRevision+"-rand")),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks becomes ready",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "kpa does not become ready without minScale endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, withMinScale(2)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric(testNamespace, testRevision, ""),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActivating, withMinScale(2), WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "kpa becomes ready with minScale endpoints",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActivating, withMinScale(2), WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			makeSKSPrivateEndpoints(2, testNamespace, testRevision),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric(testNamespace, testRevision, ""),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withMinScale(2), WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks does not exist",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// SKS does not exist, so we're just creating and have no status.
			Object: kpa(testNamespace, testRevision, markActivating),
		}},
		WantCreates: []runtime.Object{
			sks(testNamespace, testRevision, WithDeployRef(deployName)),
		},
	}, {
		Name: "sks is out of whack",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive),
			sks(testNamespace, testRevision, WithDeployRef("bar"),
				WithPubService),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// SKS just got updated and we don't have up to date status.
			Object: kpa(testNamespace, testRevision, markActivating, WithPAStatusService(testRevision)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithPubService,
				WithDeployRef(deployName)),
		}},
	}, {
		Name: "sks cannot be created",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
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
			kpa(testNamespace, testRevision, markActive),
			sks(testNamespace, testRevision, WithDeployRef("bar")),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
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
			kpa(testNamespace, testRevision, markActive),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady,
				WithSKSOwnersRemoved),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
			metric(testNamespace, testRevision, ""),
			expectedDeploy,
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markResourceNotOwned("ServerlessService", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: PA: test-revision does not own SKS: test-revision"),
		},
	}, {
		Name: "steady not serving",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision),
				withMSvcStatus("my-my-hey-hey")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("my-my-hey-hey")),
			metric(testNamespace, testRevision, "my-my-hey-hey"),
			deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
				d.Spec.Replicas = ptr.Int32(0)
			}),
			// Should be present, but empty.
			makeSKSPrivateEndpoints(0, testNamespace, testRevision),
		},
	}, {
		Name: "steady not serving (scale to zero)",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision),
				withMSvcStatus("out-of-the-blue")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("out-of-the-blue")),
			metric(testNamespace, testRevision, "out-of-the-blue"),
			deploy(testNamespace, testRevision),
			// Should be present, but empty.
			makeSKSPrivateEndpoints(0, testNamespace, testRevision),
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
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, markOld,
				WithPAStatusService(testRevision),
				withMSvcStatus("and-into-the-black")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("and-into-the-black")),
			metric(testNamespace, testRevision, "and-into-the-black"),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				WithPAStatusService(testRevision), withMSvcStatus("and-into-the-black")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
	}, {
		Name: "scaling to 0, but not stable for long enough, so no-op",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus("but-you-pay-for-that")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("but-you-pay-for-that")),
			metric(testNamespace, testRevision, "but-you-pay-for-that"),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
	}, {
		Name: "activation failure",
		Key:  key,
		Ctx:  context.WithValue(context.Background(), deciderKey, decider(testNamespace, testRevision, 0)),
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActivating, markOld,
				WithPAStatusService(testRevision),
				withMSvcStatus("once-you're-gone")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("once-you're-gone")),
			metric(testNamespace, testRevision, "once-you're-gone"),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision,
				WithNoTraffic("TimedOut", "The target could not be activated."),
				WithPAStatusService(testRevision), withMSvcStatus("once-you're-gone")),
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
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		// TODO(vagababov): see if we can get rid of the static piece of configuration and
		// constant namespace and revision names.
		// Make sure we don't want to scale to 0.

		// Make new decider if it's not in the context
		if ctx.Value(deciderKey) == nil {
			decider := resources.MakeDecider(
				ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "trying-hard-to-care-in-this-test")
			decider.Status.DesiredScale = desiredScale
			decider.Generation = 2112
			fakeDeciders.Create(ctx, decider)
		} else {
			fakeDeciders.Create(ctx, ctx.Value(deciderKey).(*autoscaler.Decider))
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
	defer logtesting.ClearAll()
	ctx, informers := SetupFakeContext(t)
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

	ctx, cancel := context.WithCancel(ctx)
	grp := errgroup.Group{}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		t.Fatalf("failed to start informers: %v", err)
	}
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
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

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
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

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
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

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

	got := perrors.Cause(ctl.Reconciler.Reconcile(context.Background(), key))
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestControllerUpdateError(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

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

	got := perrors.Cause(ctl.Reconciler.Reconcile(context.Background(), key))
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestControllerGetError(t *testing.T) {
	defer logtesting.ClearAll()
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

	got := perrors.Cause(ctl.Reconciler.Reconcile(context.Background(), key))
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestScaleFailure(t *testing.T) {
	defer logtesting.ClearAll()
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

func (km *testDeciders) Watch(fn func(string)) {}

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

func (km *failingDeciders) Watch(fn func(string)) {
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
	s := sks(testNamespace, testRevision, WithPrivateService(n+"-rand"))
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: s.Namespace,
			Name:      s.Status.PrivateServiceName,
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

func decider(ns, name string, desiredScale int32) *autoscaler.Decider {
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
			DesiredScale: desiredScale,
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
