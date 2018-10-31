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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/configmap"
	ctrl "github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/activator"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/gc"
	rclr "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

const (
	testNamespace       = "test"
	defaultDomainSuffix = "test-domain.dev"
	prodDomainSuffix    = "prod-domain.com"
)

func getTestRouteWithTrafficTargets(traffic []v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
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
}

func getTestRevision(name string) *v1alpha1.Revision {
	return getTestRevisionWithCondition(name, duckv1alpha1.Condition{
		Type:   v1alpha1.RevisionConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "ServiceReady",
	})
}

func getTestRevisionWithCondition(name string, cond duckv1alpha1.Condition) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/serving/v1alpha1/namespaces/test/revisions/%s", name),
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
				Image: "test-image",
			},
		},
		Status: v1alpha1.RevisionStatus{
			ServiceName: fmt.Sprintf("%s-service", name),
			Conditions:  duckv1alpha1.Conditions{cond},
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
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
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
		Spec: *config.Spec.RevisionTemplate.Spec.DeepCopy(),
		Status: v1alpha1.RevisionStatus{
			ServiceName: "p-deadbeef-service",
		},
	}
	rev.Status.MarkResourcesAvailable()
	rev.Status.MarkContainerHealthy()
	return rev
}

func getActivatorDestinationWeight(w int) v1alpha3.DestinationWeight {
	return v1alpha3.DestinationWeight{
		Destination: v1alpha3.Destination{
			Host: rclr.GetK8sServiceFullname(activator.K8sServiceName, system.Namespace),
			Port: v1alpha3.PortSelector{
				Number: 80,
			},
		},
		Weight: w,
	}
}

func newTestReconciler(t *testing.T, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	reconciler *Reconciler,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher *configmap.ManualWatcher) {
	kubeClient, servingClient, _, reconciler, kubeInformer, servingInformer, configMapWatcher = newTestSetup(t)
	return
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *ctrl.Impl,
	reconciler *Reconciler,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher *configmap.ManualWatcher) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	cms := []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.DomainConfigName,
				Namespace: system.Namespace,
			},
			Data: map[string]string{
				defaultDomainSuffix: "",
				prodDomainSuffix:    "selector:\n  app: prod",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gc.ConfigName,
				Namespace: system.Namespace,
			},
			Data: map[string]string{},
		},
	}
	for _, cm := range configs {
		cms = append(cms, cm)
	}

	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace}
	servingClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)

	controller = NewController(
		rclr.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		servingInformer.Serving().V1alpha1().Routes(),
		servingInformer.Serving().V1alpha1().Configurations(),
		servingInformer.Serving().V1alpha1().Revisions(),
		kubeInformer.Core().V1().Services(),
		servingInformer.Networking().V1alpha1().ClusterIngresses(),
	)

	reconciler = controller.Reconciler.(*Reconciler)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return
}

func getRouteIngressFromClient(t *testing.T, servingClient *fakeclientset.Clientset, route *v1alpha1.Route) *netv1alpha1.ClusterIngress {
	opts := metav1.ListOptions{
		LabelSelector: labels.Set(map[string]string{
			serving.RouteLabelKey:          route.Name,
			serving.RouteNamespaceLabelKey: route.Namespace,
		}).AsSelector().String(),
	}
	cis, err := servingClient.NetworkingV1alpha1().ClusterIngresses().List(opts)
	if err != nil {
		t.Errorf("ClusterIngress.Get(%v) = %v", opts, err)
	}

	if len(cis.Items) != 1 {
		t.Errorf("ClusterIngress.Get(%v), expect 1 instance, but got %d", opts, len(cis.Items))
	}

	return &cis.Items[0]
}

func addResourcesToInformers(
	t *testing.T, servingClient *fakeclientset.Clientset,
	servingInformer informers.SharedInformerFactory, route *v1alpha1.Route) {
	t.Helper()

	ns := route.Namespace

	route, err := servingClient.ServingV1alpha1().Routes(ns).Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Route.Get(%v) = %v", route.Name, err)
	}
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	ci := getRouteIngressFromClient(t, servingClient, route)
	servingInformer.Networking().V1alpha1().ClusterIngresses().Informer().GetIndexer().Add(ci)
}

// Test the only revision in the route is in Reserve (inactive) serving status.
func TestCreateRouteForOneReserveRevision(t *testing.T) {
	kubeClient, servingClient, controller, _, servingInformer, _ := newTestReconciler(t)

	h := NewHooks()
	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created service "test-route"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "^Created ClusterIngress.*" /*ingress name is unset in test*/))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Updated status for route "test-route"`))

	// An inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		duckv1alpha1.Condition{
			Type:   v1alpha1.RevisionConditionActive,
			Status: corev1.ConditionFalse,
		})
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName:      "test-rev",
			ConfigurationName: "test-config",
			Percent:           100,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(context.TODO(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, servingClient, route)

	// Check labels
	expectedLabels := map[string]string{
		serving.RouteLabelKey:          route.Name,
		serving.RouteNamespaceLabelKey: route.Namespace,
	}
	if diff := cmp.Diff(expectedLabels, ci.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	// Check owner refs
	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Route",
		Name:       route.Name,
	}}

	if diff := cmp.Diff(expectedRefs, ci.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
	}
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Rules: []netv1alpha1.ClusterIngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
				"test-route.test.svc",
				"test-route.test",
				"test-route",
			},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: "knative-serving",
							ServiceName:      "activator-service",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					AppendHeaders: map[string]string{
						"knative-serving-revision":  "test-rev",
						"knative-serving-namespace": testNamespace,
					},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
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
	servingInformer.Networking().V1alpha1().ClusterIngresses().Informer().GetIndexer().Update(ci)
	controller.Reconcile(context.TODO(), KeyOrDie(route))

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestReconciler(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	servingClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           90,
		}, {
			RevisionName: rev.Name,
			Percent:      10,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(context.TODO(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, servingClient, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Rules: []netv1alpha1.ClusterIngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
				"test-route.test.svc",
				"test-route.test",
				"test-route",
			},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", cfgrev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
					}, {
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", rev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
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
	_, servingClient, controller, _, servingInformer, _ := newTestReconciler(t)
	// A standalone inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		duckv1alpha1.Condition{
			Type:   v1alpha1.RevisionConditionActive,
			Status: corev1.ConditionFalse,
		})
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	servingClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           90,
		}, {
			RevisionName:      rev.Name,
			ConfigurationName: "test-config",
			Percent:           10,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(context.TODO(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, servingClient, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Rules: []netv1alpha1.ClusterIngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
				"test-route.test.svc",
				"test-route.test",
				"test-route",
			},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", cfgrev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 90,
					}, {
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: "knative-serving",
							ServiceName:      "activator-service",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 10,
					}},
					AppendHeaders: map[string]string{
						"knative-serving-revision":  "test-rev",
						"knative-serving-namespace": testNamespace,
					},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
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
	_, servingClient, controller, _, servingInformer, _ := newTestReconciler(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	servingClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(cfgrev)

	// A route with duplicate targets. These will be deduped.
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: "test-config",
			Percent:           30,
		}, {
			ConfigurationName: "test-config",
			Percent:           20,
		}, {
			RevisionName: "test-rev",
			Percent:      10,
		}, {
			RevisionName: "test-rev",
			Percent:      5,
		}, {
			Name:         "test-revision-1",
			RevisionName: "test-rev",
			Percent:      10,
		}, {
			Name:         "test-revision-1",
			RevisionName: "test-rev",
			Percent:      10,
		}, {
			Name:         "test-revision-2",
			RevisionName: "test-rev",
			Percent:      15,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(context.TODO(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, servingClient, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Rules: []netv1alpha1.ClusterIngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
				"test-route.test.svc",
				"test-route.test",
				"test-route",
			},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", cfgrev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}, {
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", rev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
					},
				}},
			},
		}, {
			Hosts: []string{"test-revision-1." + domain},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev-service",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
					},
				}},
			},
		}, {
			Hosts: []string{"test-revision-2." + domain},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      "test-rev-service",
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
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
	_, servingClient, controller, _, servingInformer, _ := newTestReconciler(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.SetLatestCreatedRevisionName(cfgrev.Name)
	config.Status.SetLatestReadyRevisionName(cfgrev.Name)
	servingClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(cfgrev)

	// A route targeting both the config and standalone revision with named
	// targets
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			Name:         "foo",
			RevisionName: "test-rev",
			Percent:      50,
		}, {
			Name:              "bar",
			ConfigurationName: "test-config",
			Percent:           50,
		}},
	)

	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(context.TODO(), KeyOrDie(route))

	ci := getRouteIngressFromClient(t, servingClient, route)
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	expectedSpec := netv1alpha1.IngressSpec{
		Rules: []netv1alpha1.ClusterIngressRule{{
			Hosts: []string{
				domain,
				"test-route.test.svc.cluster.local",
				"test-route.test.svc",
				"test-route.test",
				"test-route",
			},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", rev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}, {
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", cfgrev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 50,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
					},
				}},
			},
		}, {
			Hosts: []string{"bar." + domain},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", cfgrev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
					},
				}},
			},
		}, {
			Hosts: []string{"foo." + domain},
			HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
				Paths: []netv1alpha1.HTTPClusterIngressPath{{
					Splits: []netv1alpha1.ClusterIngressBackendSplit{{
						ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
							ServiceNamespace: testNamespace,
							ServiceName:      fmt.Sprintf("%s-service", rev.Name),
							ServicePort:      intstr.FromInt(80),
						},
						Percent: 100,
					}},
					Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Retries: &netv1alpha1.HTTPRetry{
						PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
						Attempts:      netv1alpha1.DefaultRetryCount,
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
	_, servingClient, controller, _, servingInformer, watcher := newTestReconciler(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := servingClient.ServingV1alpha1().Routes(route.Namespace)

	// Create a route.
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	routeClient.Create(route)
	controller.Reconcile(context.TODO(), KeyOrDie(route))
	addResourcesToInformers(t, servingClient, servingInformer, route)

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
					Namespace: system.Namespace,
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
					Namespace: system.Namespace,
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
		// An invalid config map
		expectedDomainSuffix: "newdefault.net",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace,
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
		expectation.apply()
		servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
		routeClient.Update(route)
		controller.Reconcile(context.TODO(), KeyOrDie(route))
		addResourcesToInformers(t, servingClient, servingInformer, route)

		route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
		expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, expectation.expectedDomainSuffix)
		if route.Status.Domain != expectedDomain {
			t.Errorf("Expected domain %q but saw %q", expectedDomain, route.Status.Domain)
		}
	}
}

func TestGlobalResyncOnUpdateDomainConfigMap(t *testing.T) {
	_, servingClient, controller, _, kubeInformer, servingInformer, watcher := newTestSetup(t)

	stopCh := make(chan struct{})
	defer func() {
		close(stopCh)
	}()

	servingInformer.Start(stopCh)
	kubeInformer.Start(stopCh)
	if err := watcher.Start(stopCh); err != nil {
		t.Fatalf("failed to start configuration manager: %v", err)
	}

	go controller.Run(1, stopCh)

	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := servingClient.ServingV1alpha1().Routes(route.Namespace)
	routeWatcher, err := routeClient.Watch(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Could not create route watcher")
	}
	defer routeWatcher.Stop()

	routeModifiedCh := make(chan struct{})
	defer close(routeModifiedCh)

	go func() {
		for event := range routeWatcher.ResultChan() {
			if event.Type == watch.Modified {
				routeModifiedCh <- struct{}{}
			}
		}
	}()

	// Create a route.
	route.Labels = map[string]string{"app": "prod"}
	routeClient.Create(route)

	// Test changes in domain config map. Routes should get updated appropriately.
	// We're expecting exactly one route modification per config-map change.
	tests := []struct {
		doThings             func()
		expectedDomainSuffix string
	}{{
		expectedDomainSuffix: prodDomainSuffix,
		doThings:             func() {}, // The update will still happen: status will be updated to match the route labels
	}, {
		expectedDomainSuffix: "mytestdomain.com",
		doThings: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace,
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
		doThings: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace,
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
		doThings: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.DomainConfigName,
					Namespace: system.Namespace,
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
				},
			}
			watcher.OnChange(&domainConfig)
		},
	}}
	for _, test := range tests {
		t.Run(test.expectedDomainSuffix, func(t *testing.T) {
			test.doThings()

			select {
			case <-routeModifiedCh:
			case <-time.NewTimer(10 * time.Second).C:
				t.Logf("routeWatcher did not receive a Type==Modified event for %s in 10s", test.expectedDomainSuffix)
			}

			route, err := routeClient.Get(route.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Error getting a route, test: '%s'", test.expectedDomainSuffix)
			}
			expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, test.expectedDomainSuffix)
			if route.Status.Domain != expectedDomain {
				t.Errorf("Expected domain %q but saw %q", expectedDomain, route.Status.Domain)
			}
		})
	}
}
