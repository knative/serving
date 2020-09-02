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
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	fakeingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakecfginformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	fakerouteinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	cfgmap "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"

	_ "knative.dev/pkg/metrics/testing"
	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/testing/v1"
)

const (
	testNamespace       = "test"
	defaultDomainSuffix = "test-domain.dev"
	prodDomainSuffix    = "prod-domain.com"
)

func testConfiguration() *v1.Configuration {
	return &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1.ConfigurationSpec{},
	}
}

func revisionForConfig(config *v1.Configuration) *v1.Revision {
	return Revision(testNamespace, "p-deadbeef", func(r *v1.Revision) {
		r.Spec = *config.Spec.GetTemplate().Spec.DeepCopy()
	}, MarkRevisionReady, WithK8sServiceName("p-deadbeef"))
}

func newTestSetup(t *testing.T, opts ...reconcilerOption) (
	ctx context.Context,
	informers []controller.Informer,
	ctrl *controller.Impl,
	configMapWatcher *configmap.ManualWatcher,
	cf context.CancelFunc) {

	ctx, cf, informers = SetupFakeContextWithCancel(t)
	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}
	ctrl = newControllerWithClock(ctx, configMapWatcher, system.RealClock{}, opts...)

	for _, cfg := range []*corev1.ConfigMap{{
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
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.ConfigName,
			Namespace: system.Namespace(),
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
	}} {
		configMapWatcher.OnChange(cfg)
	}

	// The Reconciler won't do any work until it becomes the leader.
	if la, ok := ctrl.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	}

	return
}

func getRouteIngressFromClient(ctx context.Context, t *testing.T, route *v1.Route) *v1alpha1.Ingress {
	t.Helper()
	opts := metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			serving.RouteLabelKey:          route.Name,
			serving.RouteNamespaceLabelKey: route.Namespace,
		}).String(),
	}
	ingresses, err := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Ingresses(route.Namespace).List(opts)
	if err != nil {
		t.Errorf("Ingress.Get(%v) = %v", opts, err)
	}

	if len(ingresses.Items) != 1 {
		t.Fatalf("Ingress.Get(%v), expect 1 instance, but got %d", opts, len(ingresses.Items))
	}

	return &ingresses.Items[0]
}

func addRouteToInformers(ctx context.Context, t *testing.T, route *v1.Route) {
	t.Helper()

	ns := route.Namespace

	route, err := fakeservingclient.Get(ctx).ServingV1().Routes(ns).Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Route.Get(%v) = %v", route.Name, err)
	}
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	if ci := getRouteIngressFromClient(ctx, t, route); ci != nil {
		fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ci)
	}
	ingress := getRouteIngressFromClient(ctx, t, route)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ingress)
}

// Test the only revision in the route is in Reserve (inactive) serving status.
func TestCreateRouteForOneReserveRevision(t *testing.T) {
	ctx, _, ctl, _, cf := newTestSetup(t)
	defer cf()

	fakeRecorder := controller.GetEventRecorder(ctx).(*record.FakeRecorder)

	// An inactive revision.
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady,
		MarkInactive("NoTraffic", "no message"))

	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A route targeting the revision
	route := Route(testNamespace, "test-route", WithSpecTraffic(v1.TrafficTarget{
		RevisionName:      "test-rev",
		ConfigurationName: "test-config",
		Percent:           ptr.Int64(100),
	}), WithRouteLabel(map[string]string{"route": "test-route"}))
	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)

	// Check labels
	expectedLabels := map[string]string{
		serving.RouteLabelKey:          route.Name,
		serving.RouteNamespaceLabelKey: route.Namespace,
		"route":                        "test-route",
	}
	if diff := cmp.Diff(expectedLabels, ci.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got):\n%s", diff)
	}

	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  "test-rev",
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
		}, {
			Hosts: []string{
				domain,
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  "test-rev",
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
		}},
	}
	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	// Update ingress loadbalancer to trigger placeholder service creation.
	ci.Status = v1alpha1.IngressStatus{
		LoadBalancer: &v1alpha1.LoadBalancerStatus{
			Ingress: []v1alpha1.LoadBalancerIngressStatus{{
				DomainInternal: "test-domain",
			}},
		},
	}
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Update(ci)
	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

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
		const wantPrefix = `Normal Created Created Ingress`
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("<-Events = %s, wanted prefix %s", got, wantPrefix)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for expected events.")
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	ctx, informers, ctl, _, cf := newTestSetup(t)
	wicb, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Error starting informers:", err)
	}
	defer func() {
		cf()
		wicb()
	}()
	// A standalone revision.
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := testConfiguration()
	cfgrev := revisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision.
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			ConfigurationName: config.Name,
			Percent:           ptr.Int64(90),
		}, v1.TrafficTarget{
			RevisionName: rev.Name,
			Percent:      ptr.Int64(10),
		}))
	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer.
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				domain,
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

// Test one out of multiple target revisions is in Reserve serving state.
func TestCreateRouteWithOneTargetReserve(t *testing.T) {
	ctx, _, ctl, _, cf := newTestSetup(t)
	defer cf()
	// A standalone inactive revision
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady,
		MarkInactive("NoTraffic", "no message"))

	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := testConfiguration()
	cfgrev := revisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			ConfigurationName: config.Name,
			Percent:           ptr.Int64(90),
		}, v1.TrafficTarget{
			RevisionName:      rev.Name,
			ConfigurationName: "test-config",
			Percent:           ptr.Int64(10),
		}))
	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				domain,
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Status.ServiceName,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}},
	}
	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	ctx, _, ctl, _, cf := newTestSetup(t)
	defer cf()

	// A standalone revision
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady, WithK8sServiceName("test-rev"))
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := testConfiguration()
	cfgrev := revisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route with duplicate targets. These will be deduped.
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			ConfigurationName: "test-config",
			Percent:           ptr.Int64(30),
		}, v1.TrafficTarget{
			ConfigurationName: "test-config",
			Percent:           ptr.Int64(20),
		}, v1.TrafficTarget{
			RevisionName: "test-rev",
			Percent:      ptr.Int64(10),
		}, v1.TrafficTarget{
			RevisionName: "test-rev",
			Percent:      ptr.Int64(5),
		}, v1.TrafficTarget{
			Tag:          "test-revision-1",
			RevisionName: "test-rev",
			Percent:      ptr.Int64(10),
		}, v1.TrafficTarget{
			Tag:          "test-revision-1",
			RevisionName: "test-rev",
			Percent:      ptr.Int64(10),
		}, v1.TrafficTarget{
			Tag:          "test-revision-2",
			RevisionName: "test-rev",
			Percent:      ptr.Int64(15),
		}))
	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				domain,
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"test-revision-1-test-route.test",
				"test-revision-1-test-route.test.svc",
				"test-revision-1-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"test-revision-1-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"test-revision-2-test-route.test",
				"test-revision-2-test-route.test.svc",
				"test-revision-2-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"test-revision-2-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		fmt.Printf("%+v\n", ci.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithNamedTargets(t *testing.T) {
	ctx, _, ctl, _, cf := newTestSetup(t)
	defer cf()
	// A standalone revision
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady, WithK8sServiceName("test-rev"))
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := testConfiguration()
	cfgrev := revisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision with named
	// targets
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			Tag:          "foo",
			RevisionName: "test-rev",
			Percent:      ptr.Int64(50),
		}, v1.TrafficTarget{
			Tag:               "bar",
			ConfigurationName: "test-config",
			Percent:           ptr.Int64(50),
		}))

	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				domain,
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"bar-test-route.test",
				"bar-test-route.test.svc",
				"bar-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"bar-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"foo-test-route.test",
				"foo-test-route.test.svc",
				"foo-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"foo-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}},
	}

	if !cmp.Equal(expectedSpec, ci.Spec) {
		t.Error("Unexpected rule spec diff (-want +got):", cmp.Diff(expectedSpec, ci.Spec))
	}
}

func TestCreateRouteWithNamedTargetsAndTagBasedRouting(t *testing.T) {
	ctx, _, ctl, watcher, cf := newTestSetup(t)
	defer cf()

	watcher.OnChange(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"tag-header-based-routing": "enabled",
		},
	})
	// A standalone revision
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady, WithK8sServiceName("test-rev"))
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(rev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration reconciler.
	config := testConfiguration()
	cfgrev := revisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	fakeservingclient.Get(ctx).ServingV1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakecfginformer.Get(ctx).Informer().GetIndexer().Add(config)
	fakeservingclient.Get(ctx).ServingV1().Revisions(testNamespace).Create(cfgrev)
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision with named
	// targets
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			Tag:          "foo",
			RevisionName: "test-rev",
			Percent:      ptr.Int64(50),
		}, v1.TrafficTarget{
			Tag:               "bar",
			ConfigurationName: "test-config",
			Percent:           ptr.Int64(50),
		}))

	fakeservingclient.Get(ctx).ServingV1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)
	ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route))

	ci := getRouteIngressFromClient(ctx, t, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := v1alpha1.IngressSpec{
		TLS: []v1alpha1.IngressTLS{},
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test",
				"test-route.test.svc",
				"test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Headers: map[string]v1alpha1.HeaderMatch{
						network.TagHeaderName: {
							Exact: "bar",
						},
					},
					Splits: []v1alpha1.IngressBackendSplit{
						{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test",
								ServiceName:      "p-deadbeef",
								ServicePort:      intstr.IntOrString{IntVal: 80},
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"Knative-Serving-Namespace": "test",
								"Knative-Serving-Revision":  "p-deadbeef",
							},
						},
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}, {
					Headers: map[string]v1alpha1.HeaderMatch{
						network.TagHeaderName: {
							Exact: "foo",
						},
					},
					Splits: []v1alpha1.IngressBackendSplit{
						{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test",
								ServiceName:      "test-rev",
								ServicePort:      intstr.IntOrString{IntVal: 80},
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"Knative-Serving-Namespace": "test",
								"Knative-Serving-Revision":  "test-rev",
							},
						},
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}, {
					AppendHeaders: map[string]string{
						network.DefaultRouteHeaderName: "true",
					},
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				domain,
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Headers: map[string]v1alpha1.HeaderMatch{
						network.TagHeaderName: {
							Exact: "bar",
						},
					},
					Splits: []v1alpha1.IngressBackendSplit{
						{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test",
								ServiceName:      "p-deadbeef",
								ServicePort:      intstr.IntOrString{IntVal: 80},
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"Knative-Serving-Namespace": "test",
								"Knative-Serving-Revision":  "p-deadbeef",
							},
						},
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}, {
					Headers: map[string]v1alpha1.HeaderMatch{
						network.TagHeaderName: {
							Exact: "foo",
						},
					},
					Splits: []v1alpha1.IngressBackendSplit{
						{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test",
								ServiceName:      "test-rev",
								ServicePort:      intstr.IntOrString{IntVal: 80},
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"Knative-Serving-Namespace": "test",
								"Knative-Serving-Revision":  "test-rev",
							},
						},
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}, {
					AppendHeaders: map[string]string{
						network.DefaultRouteHeaderName: "true",
					},
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"bar-test-route.test",
				"bar-test-route.test.svc",
				"bar-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					AppendHeaders: map[string]string{
						network.TagHeaderName: "bar",
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"bar-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      cfgrev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  cfgrev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					AppendHeaders: map[string]string{
						network.TagHeaderName: "bar",
					},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}, {
			Hosts: []string{
				"foo-test-route.test",
				"foo-test-route.test.svc",
				"foo-test-route.test.svc.cluster.local",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
					AppendHeaders: map[string]string{
						network.TagHeaderName: "foo",
					},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityClusterLocal,
		}, {
			Hosts: []string{
				"foo-test-route.test.test-domain.dev",
			},
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      rev.Name,
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
						AppendHeaders: map[string]string{
							"Knative-Serving-Revision":  rev.Name,
							"Knative-Serving-Namespace": testNamespace,
						},
					}},
					Timeout: &metav1.Duration{Duration: 48 * time.Hour},
					AppendHeaders: map[string]string{
						network.TagHeaderName: "foo",
					},
				}},
			},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		}},
	}

	if diff := cmp.Diff(expectedSpec, ci.Spec); diff != "" {
		fmt.Printf("%+v\n", ci.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestUpdateDomainConfigMap(t *testing.T) {
	templateCM := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			"mytestdomain.com":  "selector:\n  app: prod",
		},
	}

	// Test changes in domain config map. Routes should get updated appropriately.
	expectations := []struct {
		apply                func(*v1.Route, *configmap.ManualWatcher)
		expectedDomainSuffix string
	}{{
		expectedDomainSuffix: prodDomainSuffix,
		apply:                func(*v1.Route, *configmap.ManualWatcher) {},
	}, {
		expectedDomainSuffix: "mytestdomain.com",
		apply: func(_ *v1.Route, watcher *configmap.ManualWatcher) {
			watcher.OnChange(&templateCM)
		},
	}, {
		expectedDomainSuffix: "newdefault.net",
		apply: func(r *v1.Route, watcher *configmap.ManualWatcher) {
			templateCM.Data = map[string]string{
				"newdefault.net":   "",
				"mytestdomain.com": "selector:\n  app: prod",
			}
			watcher.OnChange(&templateCM)
			r.Labels = nil
		},
	}, {
		// When no domain with an open selector is specified, we fallback
		// on the default of example.com.
		expectedDomainSuffix: config.DefaultDomain,
		apply: func(r *v1.Route, watcher *configmap.ManualWatcher) {
			templateCM.Data = map[string]string{
				"mytestdomain.com": "selector:\n  app: prod",
			}
			watcher.OnChange(&templateCM)
			r.Labels = nil
		},
	}}

	for _, tc := range expectations {
		t.Run(tc.expectedDomainSuffix, func(t *testing.T) {
			ctx, ifs, ctl, watcher, cf := newTestSetup(t)
			waitInformers, err := controller.RunInformers(ctx.Done(), ifs...)
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}
			defer func() {
				cf()
				waitInformers()
			}()
			route := Route(testNamespace, uuid.New().String(), WithRouteGeneration(1982),
				WithRouteLabel(map[string]string{"app": "prod"}))
			routeClient := fakeservingclient.Get(ctx).ServingV1().Routes(route.Namespace)

			// Create a route.
			fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)
			routeClient.Create(route)
			if err := ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route)); err != nil {
				t.Fatal("Reconcile() =", err)
			}
			addRouteToInformers(ctx, t, route)

			// Wait initial reconcile to finish.
			rl := fakerouteinformer.Get(ctx).Lister().Routes(route.Namespace)
			if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
				r, err := rl.Get(route.Name)
				if err != nil {
					return false, err
				}
				return !cmp.Equal(r.Status, route.Status) && r.Generation == r.Status.ObservedGeneration, nil
			}); err != nil {
				t.Fatal("Failed to see route initial reconcile propagation:", err)
			}

			// Ensure we're operating on a copy, to make sure the update below
			// generates an actual diff. Otherwise the value in the
			// fake cache is going to be changed. Now, this update sometimes races with
			// the CM update and since the update below is a noop (the same object
			// is applied), nothing happens and the test fails (for one of the tests).
			// So we'll retry the operation several times.
			timeout := time.After(10 * time.Second)
			wantDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, tc.expectedDomainSuffix)
			for i := 1; ; i++ {
				select {
				case <-timeout:
					t.Fatal("Timed out trying to see the propagation")
				default:
					t.Log("Starting iteration", i)
				}
				route, _ := rl.Get(route.Name)
				route = route.DeepCopy()
				route.Generation++
				tc.apply(route, watcher)
				if _, err := routeClient.Update(route); err != nil {
					t.Fatal("Route.Update() =", err)
				}

				// Ensure we have the proper version in the informers.
				if err := wait.PollImmediate(10*time.Millisecond, 3*time.Second, func() (bool, error) {
					r, err := rl.Get(route.Name)
					return r != nil && r.Generation == route.Generation, err
				}); err != nil {
					t.Fatal("Failed to see informers get the new Route version:", err)
				}

				// Now that we know the exact version the reconciler is going to see in the
				// informers, let's reconcile.
				if err := ctl.Reconciler.Reconcile(context.Background(), KeyOrDie(route)); err != nil {
					t.Fatal("Reconcile() =", err)
				}

				var gotDomain string
				if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
					r, err := routeClient.Get(route.Name, metav1.GetOptions{})
					if err != nil {
						return false, err
					}
					// Wait for the object to reconcile and the domain to propagate.
					if r.Status.ObservedGeneration != route.Generation && r.Status.URL == nil {
						return false, nil
					}
					gotDomain = r.Status.URL.Host
					return gotDomain == wantDomain, nil
				}); err != nil {
					t.Logf("Failed to see route domain propagation, got %s, want %s: %v", gotDomain, wantDomain, err)
				} else {
					t.Log("Success")
					break
				}
			}
		})
	}
}

func TestGlobalResyncOnUpdateDomainConfigMap(t *testing.T) {
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
			ctx, informers, ctrl, watcher, cf := newTestSetup(t)

			grp := errgroup.Group{}

			servingClient := fakeservingclient.Get(ctx)
			routeInformer := fakerouteinformer.Get(ctx)

			waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}
			defer func() {
				cf()
				if err := grp.Wait(); err != nil {
					t.Errorf("Wait() = %v", err)
				}
				waitInformers()
			}()

			if err := watcher.Start(ctx.Done()); err != nil {
				t.Fatal("failed to start configuration manager:", err)
			}

			grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

			// Create a route.
			route := Route(testNamespace, "test-route",
				WithRouteLabel(map[string]string{"app": "prod"}), WithRouteGeneration(1))

			created, err := servingClient.ServingV1().Routes(route.Namespace).Create(route)
			if err != nil {
				t.Fatal("Failed to create route", err)
			}
			routeInformer.Informer().GetIndexer().Add(created)

			rl := routeInformer.Lister()
			if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
				r, err := rl.Routes(route.Namespace).Get(route.Name)
				if err != nil {
					return false, err
				}
				// Once we see an observed generation, we know the route got initially reconciled.
				return r.Status.ObservedGeneration == 1, nil
			}); err != nil {
				t.Fatal("Failed to see route creation propagation:", err)
			}

			test.doThings(watcher)

			expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, test.expectedDomainSuffix)
			if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
				r, err := rl.Routes(route.Namespace).Get(route.Name)
				if err != nil {
					return false, err
				}

				return r.Status.URL != nil && r.Status.URL.Host == expectedDomain, nil
			}); err != nil {
				t.Fatal("Failed to see route update propagation:", err)
			}
		})
	}
}

func TestRouteDomain(t *testing.T) {
	route := Route("default", "myapp", WithRouteLabel(map[string]string{"route": "myapp"}), WithRouteAnnotation(map[string]string{"sub": "mysub"}))
	ctx := context.Background()
	cfg := ReconcilerTestConfig(false)
	ctx = config.ToContext(ctx, cfg)

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
		t.Run(test.Name, func(t *testing.T) {
			cfg.Network.DomainTemplate = test.Template

			res, err := domains.DomainNameFromTemplate(ctx, route.ObjectMeta, route.Name)

			if test.Pass != (err == nil) {
				t.Fatal("DomainNameFromTemplate supposed to fail but didn't")
			}
			if got, want := res, test.Expected; got != want {
				t.Errorf("DomainNameFromTemplate = %q, want: %q", res, test.Expected)
			}
		})
	}
}

func TestAutoTLSEnabled(t *testing.T) {
	tests := []struct {
		name                  string
		configAutoTLSEnabled  bool
		tlsDisabledAnnotation string
		wantAutoTLSEnabled    bool
	}{{
		name:                 "AutoTLS enabled by config, not disabled by annotation",
		configAutoTLSEnabled: true,
		wantAutoTLSEnabled:   true,
	}, {
		name:                  "AutoTLS enabled by config, disabled by annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                 "AutoTLS disabled by config, not disabled by annotation",
		configAutoTLSEnabled: false,
		wantAutoTLSEnabled:   false,
	}, {
		name:                  "AutoTLS disabled by config, disabled by annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                  "AutoTLS enabled by config, invalid annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    true,
	}, {
		name:                  "AutoTLS disabled by config, invalid annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    false,
	}}

	r := Route("test-ns", "test-route")
	r.Annotations = map[string]string{}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := config.ToContext(context.Background(), &config.Config{
				Network: &network.Config{
					AutoTLS: test.configAutoTLSEnabled,
				},
			})

			r.Annotations[networking.DisableAutoTLSAnnotationKey] = test.tlsDisabledAnnotation

			if got := autoTLSEnabled(ctx, r); got != test.wantAutoTLSEnabled {
				t.Errorf("autoTLSEnabled = %t, want %t", got, test.wantAutoTLSEnabled)
			}
		})
	}
}
