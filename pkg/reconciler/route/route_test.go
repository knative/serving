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

package route

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	// Inject the informers this controller depends on.
	_ "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service/fake"
	fakeservingclient "github.com/knative/serving/pkg/client/injection/client/fake"
	_ "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	fakeciinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress/fake"
	fakecfginformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/configuration/fake"
	fakerevisioninformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	fakerouteinformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/route/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	ctrl "github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/system"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
	"github.com/knative/serving/pkg/reconciler/route/domains"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"

	. "github.com/knative/pkg/reconciler/testing"
)

const (
	testNamespace       = "test"
	defaultDomainSuffix = "test-domain.dev"
	prodDomainSuffix    = "prod-domain.com"
)

func getTestRouteWithTrafficTargets(traffic []v1alpha1.TrafficTarget) *v1alpha1.Route {
	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/Routes/test-route",
			Name:      "test-route",
			Namespace: testNamespace,
			Labels: map[string]string{
				"route": "test-route",
			},
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
	route.SetDefaults(context.Background())
	return route
}

func getTestRevision(name string) *v1alpha1.Revision {
	return getTestRevisionWithCondition(name, apis.Condition{
		Type:   v1alpha1.RevisionConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "ServiceReady",
	})
}

func getTestRevisionWithCondition(name string, cond apis.Condition) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/serving/v1alpha1/namespaces/test/revisions/%s", name),
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			DeprecatedContainer: &corev1.Container{
				Image: "test-image",
			},
		},
		Status: v1alpha1.RevisionStatus{
			ServiceName: name,
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{cond},
			},
		},
	}
}

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisiontemplates/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			// This is a workaround for generation initialization
			DeprecatedGeneration: 1,
			DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					DeprecatedContainer: &corev1.Container{
						Image: "test-image",
					},
				},
			},
		},
	}
}

func getTestRevisionForConfig(config *v1alpha1.Configuration) *v1alpha1.Revision {
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisions/p-deadbeef",
			Name:      "p-deadbeef",
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: config.Name,
			},
		},
		Spec: *config.Spec.GetTemplate().Spec.DeepCopy(),
		Status: v1alpha1.RevisionStatus{
			ServiceName: "p-deadbeef",
		},
	}
	rev.Status.MarkResourcesAvailable()
	rev.Status.MarkContainerHealthy()
	return rev
}

func newTestReconciler(t *testing.T, configs ...*corev1.ConfigMap) (
	ctx context.Context,
	informers []controller.Informer,
	reconciler *Reconciler,
	configMapWatcher *configmap.ManualWatcher) {
	ctx, informers, _, reconciler, configMapWatcher = newTestSetup(t)
	return
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	ctx context.Context,
	informers []controller.Informer,
	controller *ctrl.Impl,
	reconciler *Reconciler,
	configMapWatcher *configmap.ManualWatcher) {

	ctx, informers = SetupFakeContext(t)
	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}
	controller = NewController(ctx, configMapWatcher)
	reconciler = controller.Reconciler.(*Reconciler)

	cms := append([]*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			prodDomainSuffix:    "selector:\n  app: prod",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}}, configs...)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return
}

func getRouteIngressFromClient(t *testing.T, ctx context.Context, route *v1alpha1.Route) *netv1alpha1.ClusterIngress {
	opts := metav1.ListOptions{
		LabelSelector: labels.Set(map[string]string{
			serving.RouteLabelKey:          route.Name,
			serving.RouteNamespaceLabelKey: route.Namespace,
		}).AsSelector().String(),
	}
	cis, err := fakeservingclient.Get(ctx).NetworkingV1alpha1().ClusterIngresses().List(opts)
	if err != nil {
		t.Errorf("ClusterIngress.Get(%v) = %v", opts, err)
	}

	if len(cis.Items) != 1 {
		t.Errorf("ClusterIngress.Get(%v), expect 1 instance, but got %d", opts, len(cis.Items))
	}

	return &cis.Items[0]
}

func getCertificateFromClient(t *testing.T, ctx context.Context, desired *netv1alpha1.Certificate) *netv1alpha1.Certificate {
	created, err := fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Certificates(%s).Get(%s) = %v", desired.Namespace, desired.Name, err)
	}
	return created
}

func addResourcesToInformers(t *testing.T, ctx context.Context, route *v1alpha1.Route) {
	t.Helper()

	ns := route.Namespace

	route, err := fakeservingclient.Get(ctx).ServingV1alpha1().Routes(ns).Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Route.Get(%v) = %v", route.Name, err)
	}
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ci := getRouteIngressFromClient(t, ctx, route)
	fakeciinformer.Get(ctx).Informer().GetIndexer().Add(ci)
}

// Test the only revision in the route is in Reserve (inactive) serving status.
func TestCreateRouteForOneReserveRevision(t *testing.T) {
	ctx, _, reconciler, _ := newTestReconciler(t)

	fakeRecorder := reconciler.Base.Recorder.(*record.FakeRecorder)

	// An inactive revision
	rev := getTestRevision("test-rev")
	rev.Status.MarkInactive("NoTraffic", "no message")

	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName:      "test-rev",
				ConfigurationName: "test-config",
				Percent:           100,
			},
		}},
	)
	fakeservingclient.Get(ctx).ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, ctx, route)

	// Check labels
	expectedLabels := map[string]string{
		serving.RouteLabelKey:          route.Name,
		serving.RouteNamespaceLabelKey: route.Namespace,
	}
	if diff := cmp.Diff(expectedLabels, ci.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		TLS:        []netv1alpha1.IngressTLS{},
		Rules: []netv1alpha1.IngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "test-rev",
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}},
	}
	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	// Update ingress loadbalancer to trigger placeholder service creation.
	ci.Status = netv1alpha1.IngressStatus{
		LoadBalancer: &netv1alpha1.LoadBalancerStatus{
			Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
				DomainInternal: "test-domain",
			}},
		},
	}
	fakeciinformer.Get(ctx).Informer().GetIndexer().Update(ci)
	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	select {
	case got := <-fakeRecorder.Events:
		const want = `Normal Created Created placeholder service "test-route"`
		if got != want {
			t.Errorf("<-Events = %s, wanted %s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for expected events.")
	}
	select {
	case got := <-fakeRecorder.Events:
		const wantPrefix = `Normal Created Created ClusterIngress`
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("<-Events = %s, wanted prefix %s", got, wantPrefix)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for expected events.")
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	ctx, _, reconciler, _ := newTestReconciler(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision.
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           90,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      10,
			},
		}},
	)
	fakeservingclient.Get(ctx).ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer.
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, ctx, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		TLS:        []netv1alpha1.IngressTLS{},
		Rules: []netv1alpha1.IngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
					}, {
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  cfgrev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

// Test one out of multiple target revisions is in Reserve serving state.
func TestCreateRouteWithOneTargetReserve(t *testing.T) {
	ctx, _, reconciler, _ := newTestReconciler(t)
	// A standalone inactive revision
	rev := getTestRevision("test-rev")
	rev.Status.MarkInactive("NoTraffic", "no message")

	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           90,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName:      rev.Name,
				ConfigurationName: "test-config",
				Percent:           10,
			},
		}},
	)
	fakeservingclient.Get(ctx).ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, ctx, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		TLS:        []netv1alpha1.IngressTLS{},
		Rules: []netv1alpha1.IngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
					}, {
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  "test-rev",
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}},
	}
	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	ctx, _, reconciler, _ := newTestReconciler(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route with duplicate targets. These will be deduped.
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: "test-config",
				Percent:           30,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				ConfigurationName: "test-config",
				Percent:           20,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      10,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      5,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:          "test-revision-1",
				RevisionName: "test-rev",
				Percent:      10,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:          "test-revision-1",
				RevisionName: "test-rev",
				Percent:      10,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:          "test-revision-2",
				RevisionName: "test-rev",
				Percent:      15,
			},
		}},
	)
	fakeservingclient.Get(ctx).ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, ctx, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		TLS:        []netv1alpha1.IngressTLS{},
		Rules: []netv1alpha1.IngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}, {
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  rev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}, {
			Hosts: []string{
				"test-revision-1-test-route.test.test-domain.dev",
				"test-revision-1." + domain,
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  rev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}, {
			Hosts: []string{
				"test-revision-2-test-route.test.test-domain.dev",
				"test-revision-2." + domain,
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  rev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		fmt.Printf("%+v\n", ci.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithNamedTargets(t *testing.T) {
	ctx, _, reconciler, _ := newTestReconciler(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision with named
	// targets
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:          "foo",
				RevisionName: "test-rev",
				Percent:      50,
			},
		}, {
			TrafficTarget: v1beta1.TrafficTarget{
				Tag:               "bar",
				ConfigurationName: "test-config",
				Percent:           50,
			},
		}},
	)

	fakeservingclient.Get(ctx).ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, ctx, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Visibility: netv1alpha1.IngressVisibilityExternalIP,
		TLS:        []netv1alpha1.IngressTLS{},
		Rules: []netv1alpha1.IngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}, {
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  cfgrev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}, {
			Hosts: []string{
				"bar-test-route.test.test-domain.dev",
				"bar." + domain,
			},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  cfgrev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}, {
			Hosts: []string{
				"foo-test-route.test.test-domain.dev",
				"foo." + domain},
			HTTP: &netv1alpha1.HTTPIngressRuleValue{
				Paths: []netv1alpha1.HTTPIngressPath{{
					Splits: []netv1alpha1.IngressBackendSplit{{
						IngressBackend: netv1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"Knative-Serving-Revision":  rev.Name,
						"Knative-Serving-Namespace": testNamespace,
					},
				}},
			},
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		fmt.Printf("%+v\n", ci.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestUpdateDomainConfigMap(t *testing.T) {
	ctx, _, reconciler, watcher := newTestReconciler(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := fakeservingclient.Get(ctx).ServingV1alpha1().Routes(route.Namespace)

	// Create a route.
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)
	routeClient.Create(route)
	reconciler.Reconcile(context.Background(), KeyOrDie(route))
	addResourcesToInformers(t, ctx, route)

	route.ObjectMeta.Labels = map[string]string{"app": "prod"}

	// Test changes in domain config map. Routes should get updated appropriately.
	expectations := []struct {
		apply                func()
		expectedDomainSuffix string
	}{{
		expectedDomainSuffix: prodDomainSuffix,
		apply:                func() {},
	}, {
		expectedDomainSuffix: "mytestdomain.com",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
					"mytestdomain.com":  "selector:\n  app: prod",
				},
			}
			watcher.OnChange(&domainConfig)
		},
	}, {
		expectedDomainSuffix: "newdefault.net",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					"newdefault.net":   "",
					"mytestdomain.com": "selector:\n  app: prod",
				},
			}
			watcher.OnChange(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}, {
		// When no domain with an open selector is specified, we fallback
		// on the default of example.com.
		expectedDomainSuffix: config.DefaultDomain,
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					"mytestdomain.com": "selector:\n  app: prod",
				},
			}
			watcher.OnChange(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}}

	for _, expectation := range expectations {
		t.Run(expectation.expectedDomainSuffix, func(t *testing.T) {
			expectation.apply()
			fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)
			routeClient.Update(route)
			reconciler.Reconcile(context.Background(), KeyOrDie(route))
			addResourcesToInformers(t, ctx, route)

			route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
			expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, expectation.expectedDomainSuffix)
			if route.Status.URL.Host != expectedDomain {
				t.Errorf("Expected domain %q but saw %q", expectedDomain, route.Status.URL.Host)
			}
		})
	}
}

func TestGlobalResyncOnUpdateDomainConfigMap(t *testing.T) {
	defer logtesting.ClearAll()
	// Test changes in domain config map. Routes should get updated appropriately.
	// We're expecting exactly one route modification per config-map change.
	tests := []struct {
		doThings             func(*configmap.ManualWatcher)
		expectedDomainSuffix string
	}{{
		expectedDomainSuffix: prodDomainSuffix,
		doThings:             func(*configmap.ManualWatcher) {}, // The update will still happen: status will be updated to match the route labels
	}, {
		expectedDomainSuffix: "mytestdomain.com",
		doThings: func(watcher *configmap.ManualWatcher) {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
					"mytestdomain.com":  "selector:\n  app: prod",
				},
			}
			watcher.OnChange(&domainConfig)
		},
	}, {
		expectedDomainSuffix: "newprod.net",
		doThings: func(watcher *configmap.ManualWatcher) {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
					"newprod.net":       "selector:\n  app: prod",
				},
			}
			watcher.OnChange(&domainConfig)
		},
	}, {
		expectedDomainSuffix: defaultDomainSuffix,
		doThings: func(watcher *configmap.ManualWatcher) {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
				},
			}
			watcher.OnChange(&domainConfig)
		},
	}}

	for _, test := range tests {
		test := test
		t.Run(test.expectedDomainSuffix, func(t *testing.T) {
			ctx, informers, ctrl, _, watcher := newTestSetup(t)

			ctx, cancel := context.WithCancel(ctx)
			grp := errgroup.Group{}
			defer func() {
				cancel()
				if err := grp.Wait(); err != nil {
					t.Errorf("Wait() = %v", err)
				}
			}()

			servingClient := fakeservingclient.Get(ctx)
			h := NewHooks()

			// Check for ClusterIngress created as a signal that syncHandler ran
			h.OnUpdate(&servingClient.Fake, "routes", func(obj runtime.Object) HookResult {
				rt := obj.(*v1alpha1.Route)
				t.Logf("route updated: %q", rt.Name)

				expectedDomain := fmt.Sprintf("%s.%s.%s", rt.Name, rt.Namespace, test.expectedDomainSuffix)
				if rt.Status.URL.Host != expectedDomain {
					t.Logf("Expected domain %q but saw %q", expectedDomain, rt.Status.URL.Host)
					return HookIncomplete
				}

				return HookComplete
			})

			if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
				t.Fatalf("failed to start cluster ingress manager: %v", err)
			}

			if err := watcher.Start(ctx.Done()); err != nil {
				t.Fatalf("failed to start configuration manager: %v", err)
			}

			grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

			// Create a route.
			route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
			route.Labels = map[string]string{"app": "prod"}

			servingClient.ServingV1alpha1().Routes(route.Namespace).Create(route)

			test.doThings(watcher)

			if err := h.WaitForHooks(3 * time.Second); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestRouteDomain(t *testing.T) {
	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/Routes/myapp",
			Name:      "myapp",
			Namespace: "default",
			Labels: map[string]string{
				"route": "myapp",
			},
			Annotations: map[string]string{
				"sub": "mysub",
			},
		},
	}

	context := context.Background()
	cfg := ReconcilerTestConfig(false)
	context = config.ToContext(context, cfg)

	tests := []struct {
		Name     string
		Template string
		Pass     bool
		Expected string
	}{{
		Name:     "Default",
		Template: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		Pass:     true,
		Expected: "myapp.default.example.com",
	}, {
		Name:     "Dash",
		Template: "{{.Name}}-{{.Namespace}}.{{.Domain}}",
		Pass:     true,
		Expected: "myapp-default.example.com",
	}, {
		Name:     "Short",
		Template: "{{.Name}}.{{.Domain}}",
		Pass:     true,
		Expected: "myapp.example.com",
	}, {
		Name:     "SuperShort",
		Template: "{{.Name}}",
		Pass:     true,
		Expected: "myapp",
	}, {
		Name:     "Annotations",
		Template: `{{.Name}}.{{ index .Annotations "sub"}}.{{.Domain}}`,
		Pass:     true,
		Expected: "myapp.mysub.example.com",
	}, {
		// This cannot get through our validation, but verify we handle errors.
		Name:     "BadVarName",
		Template: "{{.Name}}.{{.NNNamespace}}.{{.Domain}}",
		Pass:     false,
		Expected: "",
	}}

	for _, test := range tests {
		cfg.Network.DomainTemplate = test.Template

		res, err := domains.DomainNameFromTemplate(context, route, route.Name)

		if test.Pass != (err == nil) {
			t.Fatalf("TestRouteDomain %q test: supposed to fail but didn't",
				test.Name)
		}
		if res != test.Expected {
			t.Fatalf("TestRouteDomain %q test: got: %q exp: %q",
				test.Name, res, test.Expected)
		}
	}
}
