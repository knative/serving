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
	fakedynamicclient "github.com/knative/pkg/injection/clients/dynamicclient/fake"
	fakekubeclient "github.com/knative/pkg/injection/clients/kubeclient/fake"
	fakeendpointsinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/endpoints/fake"
	fakeserviceinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service/fake"
	fakeservingclient "github.com/knative/serving/pkg/client/injection/client/fake"
	fakekpainformer "github.com/knative/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	fakesksinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	fakerevisioninformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"github.com/knative/serving/pkg/reconciler"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/ptr"
	"github.com/knative/pkg/system"
	_ "github.com/knative/pkg/system/testing"
	"github.com/knative/serving/pkg/apis/autoscaling"
	asv1a1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	rpkg "github.com/knative/serving/pkg/reconciler"
	areconciler "github.com/knative/serving/pkg/reconciler/autoscaling"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"github.com/knative/serving/pkg/reconciler/autoscaling/kpa/resources"
	aresources "github.com/knative/serving/pkg/reconciler/autoscaling/resources"
	revisionresources "github.com/knative/serving/pkg/reconciler/revision/resources"
	presources "github.com/knative/serving/pkg/resources"
	perrors "github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/testing/v1alpha1"
	. "github.com/knative/serving/pkg/testing"
)

// TODO(#3591): Convert KPA tests to table tests.

const (
	gracePeriod              = 60 * time.Second
	stableWindow             = 5 * time.Minute
	defaultConcurrencyTarget = 10.0
)

func defaultConfigMapData() map[string]string {
	return map[string]string{
		"max-scale-up-rate":                       "1.0",
		"container-concurrency-target-percentage": "0.5",
		"container-concurrency-target-default":    fmt.Sprintf("%v", defaultConcurrencyTarget),
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

func withMSvcStatus(s string) PodAutoscalerOption {
	return func(pa *asv1a1.PodAutoscaler) {
		pa.Status.MetricsServiceName = s
	}
}

func kpa(ns, n string, opts ...PodAutoscalerOption) *asv1a1.PodAutoscaler {
	rev := newTestRevision(ns, n)
	kpa := revisionresources.MakeKPA(rev)
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

func TestReconcileAndScaleToZero(t *testing.T) {
	// This test suite uses special decider that will
	// force KPA to scale to 0.
	const key = testNamespace + "/" + testRevision
	const deployName = testRevision + "-deployment"
	usualSelector := map[string]string{"a": "b"}

	table := TableTest{{
		Name: "steady not serving",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision),
				withMSvcStatus("rocking-in-the-free-world")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("rocking-in-the-free-world")),
			deploy(testNamespace, testRevision, func(d *appsv1.Deployment) {
				d.Spec.Replicas = ptr.Int32(0)
			}),
			// Should be present, but empty.
			makeSKSPrivateEndpoints(0, testNamespace, testRevision),
		},
	}, {
		Name: "steady not serving (scale to zero)",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				markOld, WithPAStatusService(testRevision),
				withMSvcStatus("out-of-the-blue")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithProxyMode, WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("out-of-the-blue")),
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
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, markOld,
				WithPAStatusService(testRevision),
				withMSvcStatus("and-into-the-black")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("and-into-the-black")),
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
		Name: "from serving to proxy, sks update fail :-(",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, markOld,
				WithPAStatusService(testRevision), withMSvcStatus("you-ask-for-this")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("you-ask-for-this")),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"error re-reconciling SKS: error updating SKS test-revision: inducing failure for update serverlessservices"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision,
				WithNoTraffic("NoTraffic", "The target is not receiving traffic."),
				WithPAStatusService(testRevision), withMSvcStatus("you-ask-for-this")),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks(testNamespace, testRevision, WithSKSReady,
				WithDeployRef(deployName), WithProxyMode),
		}},
	}, {
		Name: "scaling to 0, but not stable for long enough, so no-op",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus("but-we-give-you-that")),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("but-we-give-you-that")),
			deploy(testNamespace, testRevision),
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		// Make sure we want to scale to 0.
		decider := resources.MakeDecider(
			ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "not-important-here")
		decider.Status.DesiredScale = 0
		decider.Generation = 42
		fakeDeciders.Create(ctx, decider)

		fakeMetrics := newTestMetrics()
		psFactory := presources.NewPodScalableInformerFactory(ctx)
		scaler := newScaler(ctx, psFactory, func(interface{}, time.Duration) {})
		scaler.activatorProbe = func(*asv1a1.PodAutoscaler, http.RoundTripper) (bool, error) { return true, nil }
		return &Reconciler{
			Base: &areconciler.Base{
				Base:              rpkg.NewBase(ctx, controllerAgentName, newConfigWatcher()),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				ServiceLister:     listers.GetK8sServiceLister(),
				Metrics:           fakeMetrics,
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
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
	}, {
		Name: "metric-service-mistmatch",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, withMSvcStatus("a350-900ULR"),
				WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("b777-200LR")),
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
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus(testRevision+"-00001")),
		}},
		WantCreates: []runtime.Object{
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
		},
	}, {
		Name: "metric-service-exists-not-on-status",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive, WithPAStatusService(testRevision)),
			sks(testNamespace, testRevision, WithDeployRef(deployName), WithSKSReady),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector),
				withMSvcName("erj-e190")),
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive,
				WithPAStatusService(testRevision), withMSvcStatus("erj-e190")),
		}},
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
			expectedDeploy,
			makeSKSPrivateEndpoints(1, testNamespace, testRevision),
		},
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
			expectedDeploy,
			makeSKSPrivateEndpoints(2, testNamespace, testRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markActive, withMinScale(2), WithPAStatusService(testRevision)),
		}},
	}, {
		Name: "sks does not exist",
		Key:  key,
		Objects: []runtime.Object{
			kpa(testNamespace, testRevision, markActive),
			metricsSvc(testNamespace, testRevision, withSvcSelector(usualSelector)),
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
			expectedDeploy,
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: kpa(testNamespace, testRevision, markResourceNotOwned("ServerlessService", testRevision)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: PA: test-revision does not own SKS: test-revision"),
		},
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		fakeDeciders := newTestDeciders()
		// TODO(vagababov): see if we can get rid of the static piece of configuration and
		// constant namespace and revision names.
		// Make sure we don't want to scale to 0.
		decider := resources.MakeDecider(
			ctx, kpa(testNamespace, testRevision), defaultConfig().Autoscaler, "trying-hard-to-care-in-this-test")
		decider.Status.DesiredScale = desiredScale
		decider.Generation = 2112
		fakeDeciders.Create(ctx, decider)

		psFactory := presources.NewPodScalableInformerFactory(ctx)
		fakeMetrics := newTestMetrics()
		return &Reconciler{
			Base: &areconciler.Base{
				Base:              rpkg.NewBase(ctx, controllerAgentName, newConfigWatcher()),
				PALister:          listers.GetPodAutoscalerLister(),
				SKSLister:         listers.GetServerlessServiceLister(),
				ServiceLister:     listers.GetK8sServiceLister(),
				Metrics:           fakeMetrics,
				ConfigStore:       &testConfigStore{config: defaultConfig()},
				PSInformerFactory: psFactory,
			},
			endpointsLister: listers.GetEndpointsLister(),
			deciders:        fakeDeciders,
			scaler:          newScaler(ctx, psFactory, func(interface{}, time.Duration) {}),
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
	fakeMetrics := newTestMetrics()
	ctl := NewController(ctx, watcher, fakeDeciders, fakeMetrics, presources.NewPodScalableInformerFactory(ctx))

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

	kpa := revisionresources.MakeKPA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	// Wait for decider to be created.
	if decider, err := pollDeciders(fakeDeciders, testNamespace, testRevision, nil); err != nil {
		t.Fatalf("Failed to get decider: %v", err)
	} else if got, want := decider.Spec.TargetConcurrency, defaultConcurrencyTarget; got != want {
		t.Fatalf("TargetConcurrency = %v, want %v", got, want)
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
		return d.Spec.TargetConcurrency == concurrencyTargetAfterUpdate
	}
	if decider, err := pollDeciders(fakeDeciders, testNamespace, testRevision, cond); err != nil {
		t.Fatalf("Failed to get decider: %v", err)
	} else if got, want := decider.Spec.TargetConcurrency, concurrencyTargetAfterUpdate; got != want {
		t.Fatalf("TargetConcurrency = %v, want %v", got, want)
	}
}

func TestControllerSynchronizesCreatesAndDeletes(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	fakeDeciders := newTestDeciders()
	fakeMetrics := newTestMetrics()
	ctl := NewController(ctx, newConfigWatcher(), fakeDeciders, fakeMetrics, presources.NewPodScalableInformerFactory(ctx))

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	ep := makeSKSPrivateEndpoints(1, testNamespace, testRevision)
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
	fakeendpointsinformer.Get(ctx).Informer().GetIndexer().Add(ep)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	kpa := revisionresources.MakeKPA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

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
	fakekpainformer.Get(ctx).Informer().GetIndexer().Delete(kpa)
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if fakeDeciders.deleteCallCount.Load() == 0 {
		t.Fatal("Decider was not deleted")
	}
	if fakeMetrics.deleteCallCount.Load() == 0 {
		t.Fatal("Metric was not deleted")
	}

	if fakeDeciders.deleteBeforeCreate.Load() {
		t.Fatal("Deciders.Delete ran before OnPresent")
	}
	if fakeMetrics.deleteBeforeCreate.Load() {
		t.Fatal("Deciders.Delete ran before OnPresent")
	}
}

func TestUpdate(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	fakeDeciders := newTestDeciders()
	fakeMetrics := newTestMetrics()
	ctl := NewController(ctx, newConfigWatcher(), fakeDeciders, fakeMetrics, presources.NewPodScalableInformerFactory(ctx))

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	ep := makeSKSPrivateEndpoints(1, testNamespace, testRevision)
	fakekubeclient.Get(ctx).CoreV1().Endpoints(testNamespace).Create(ep)
	fakeendpointsinformer.Get(ctx).Informer().GetIndexer().Add(ep)

	kpa := revisionresources.MakeKPA(rev)
	kpa.SetDefaults(context.Background())
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	msvc := aresources.MakeMetricsService(kpa, map[string]string{
		serving.RevisionLabelKey: rev.Name,
	})
	fakekubeclient.Get(ctx).CoreV1().Services(testNamespace).Create(msvc)
	fakeserviceinformer.Get(ctx).Informer().GetIndexer().Add(msvc)

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
	if count := fakeMetrics.createCallCount.Load(); count != 1 {
		t.Fatalf("Metrics.Create called %d times instead of once", count)
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
	fakekpainformer.Get(ctx).Informer().GetIndexer().Update(kpa)

	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	if fakeDeciders.updateCallCount.Load() == 0 {
		t.Fatal("Deciders.Update was not called")
	}
}

func TestNonKPAClass(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	fakeDeciders := newTestDeciders()
	fakeMetrics := newTestMetrics()
	ctl := NewController(ctx, newConfigWatcher(), fakeDeciders, fakeMetrics, presources.NewPodScalableInformerFactory(ctx))

	rev := newTestRevision(testNamespace, testRevision)
	rev.Annotations = map[string]string{
		autoscaling.ClassAnnotationKey: autoscaling.HPA, // non "kpa" class
	}

	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)
	kpa := revisionresources.MakeKPA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	// Wait for the Reconcile to complete.
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	// Verify no Deciders or Metrics were created
	if fakeDeciders.createCallCount.Load() != 0 {
		t.Error("Unexpected Deciders created")
	}
	if fakeMetrics.createCallCount.Load() != 0 {
		t.Error("Unexpected Metrics created")
	}
}

func TestNoEndpoints(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	ctl := NewController(ctx, newConfigWatcher(), newTestDeciders(), newTestMetrics(), presources.NewPodScalableInformerFactory(ctx))

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	kpa := revisionresources.MakeKPA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	// Wait for the Reconcile to complete.
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	newKPA, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "Unknown" {
		t.Errorf("GetCondition(Ready) = %v, wanted Unknown", cond)
	}
}

func TestEmptyEndpoints(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	ctl := NewController(ctx, newConfigWatcher(), newTestDeciders(), newTestMetrics(), presources.NewPodScalableInformerFactory(ctx))

	rev := newTestRevision(testNamespace, testRevision)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	kpa := revisionresources.MakeKPA(rev)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	// Wait for the Reconcile to complete.
	if err := ctl.Reconciler.Reconcile(context.Background(), testNamespace+"/"+testRevision); err != nil {
		t.Errorf("Reconcile() = %v", err)
	}

	newKPA, err := fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Get(
		kpa.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Get() = %v", err)
	}
	if cond := newKPA.Status.GetCondition("Ready"); cond == nil || cond.Status != "Unknown" {
		t.Errorf("GetCondition(Ready) = %v, wanted Unknown", cond)
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
		},
		newTestMetrics(),
		presources.NewPodScalableInformerFactory(ctx),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

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
		},
		newTestMetrics(),
		presources.NewPodScalableInformerFactory(ctx),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

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
		},
		newTestMetrics(),
		presources.NewPodScalableInformerFactory(ctx),
	)

	kpa := revisionresources.MakeKPA(newTestRevision(testNamespace, testRevision))
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

	newDeployment(t, fakedynamicclient.Get(ctx), testRevision+"-deployment", 3)

	got := perrors.Cause(ctl.Reconciler.Reconcile(context.Background(), key))
	if got != want {
		t.Errorf("Reconcile() = %v, wanted %v", got, want)
	}
}

func TestScaleFailure(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	ctl := NewController(ctx, newConfigWatcher(), newTestDeciders(), newTestMetrics(), presources.NewPodScalableInformerFactory(ctx))

	// Only put the KPA in the lister, which will prompt failures scaling it.
	rev := newTestRevision(testNamespace, testRevision)
	kpa := revisionresources.MakeKPA(rev)
	fakekpainformer.Get(ctx).Informer().GetIndexer().Add(kpa)

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
		return nil, apierrors.NewNotFound(asv1a1.Resource("Deciders"), autoscaler.NewMetricKey(namespace, name))
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

func newTestRevision(namespace string, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/%s/revisions/%s", namespace, name),
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image:      "gcr.io/repo/image",
				Command:    []string{"echo"},
				Args:       []string{"hello", "world"},
				WorkingDir: "/tmp",
			},
			DeprecatedConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelSingle,
		},
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

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ reconciler.ConfigStore = (*testConfigStore)(nil)
