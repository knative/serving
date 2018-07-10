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

/* TODO tests:
- When a Route is created:
  - a namespace is created

- When a Revision is updated TODO
- When a Revision is deleted TODO
*/
import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/configmap"
	"github.com/knative/serving/pkg/system"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/serving/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	ctrl "github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/route/config"
	. "github.com/knative/serving/pkg/logging/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	"github.com/knative/serving/pkg/controller/route/resources"
	resourcenames "github.com/knative/serving/pkg/controller/route/resources/names"
	. "github.com/knative/serving/pkg/controller/testing"
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
	return getTestRevisionWithCondition(name, v1alpha1.RevisionCondition{
		Type:   v1alpha1.RevisionConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "ServiceReady",
	})
}

func getTestRevisionWithCondition(name string, cond v1alpha1.RevisionCondition) *v1alpha1.Revision {
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
			Conditions:  []v1alpha1.RevisionCondition{cond},
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
			Host: ctrl.GetK8sServiceFullname(activator.K8sServiceName, system.Namespace),
			Port: v1alpha3.PortSelector{
				Number: 80,
			},
		},
		Weight: w,
	}
}

func newTestController(t *testing.T, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher configmap.Watcher) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	var cms []*corev1.ConfigMap
	cms = append(cms, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DomainConfigName,
			Namespace: system.Namespace,
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			prodDomainSuffix:    "selector:\n  app: prod",
		},
	})
	for _, cm := range configs {
		cms = append(cms, cm)
	}

	configMapWatcher = configmap.NewFixedWatcher(cms...)
	servingClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)

	controller = NewController(
		ctrl.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		servingInformer.Serving().V1alpha1().Routes(),
		servingInformer.Serving().V1alpha1().Configurations(),
		servingInformer.Serving().V1alpha1().Revisions(),
		kubeInformer.Core().V1().Services(),
		servingInformer.Networking().V1alpha3().VirtualServices(),
	)

	return
}

func addResourcesToInformers(
	t *testing.T, kubeClient *fakekubeclientset.Clientset, kubeInformer kubeinformers.SharedInformerFactory,
	servingClient *fakeclientset.Clientset, servingInformer informers.SharedInformerFactory, route *v1alpha1.Route) {
	t.Helper()

	ns := route.Namespace

	route, err := servingClient.ServingV1alpha1().Routes(ns).Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Route.Get(%v) = %v", route.Name, err)
	}
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	vsName := resourcenames.VirtualService(route)
	virtualService, err := servingClient.NetworkingV1alpha3().VirtualServices(ns).Get(vsName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("VirtualService.Get(%v) = %v", vsName, err)
	}
	servingInformer.Networking().V1alpha3().VirtualServices().Informer().GetIndexer().Add(virtualService)

	ksName := resourcenames.K8sService(route)
	service, err := kubeClient.CoreV1().Services(ns).Get(ksName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Services.Get(%v) = %v", ksName, err)
	}
	kubeInformer.Core().V1().Services().Informer().GetIndexer().Add(service)
}

// Test the only revision in the route is in Reserve (inactive) serving status.
func TestCreateRouteForOneReserveRevision(t *testing.T) {
	kubeClient, servingClient, controller, _, servingInformer, _ := newTestController(t)

	h := NewHooks()
	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created service "test-route"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created VirtualService "test-route"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Updated status for route "test-route"`))

	// An inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
		})
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: "test-rev",
			Percent:      100,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(KeyOrDie(route))

	// Look for the route rule with activator as the destination.
	vs, err := servingClient.NetworkingV1alpha3().VirtualServices(testNamespace).Get(resourcenames.VirtualService(route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting VirtualService: %v", err)
	}

	// Check labels
	expectedLabels := map[string]string{"route": route.Name}
	if diff := cmp.Diff(expectedLabels, vs.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	// Check owner refs
	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Route",
		Name:       route.Name,
	}}

	if diff := cmp.Diff(expectedRefs, vs.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
	}
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	clusterDomain := "test-route.test.svc.cluster.local"
	expectedSpec := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress
		// Gateway, and the 'mesh' Gateway.  The former provides
		// access from outside of the cluster, and the latter provides
		// access for services from inside the cluster.
		Gateways: []string{
			resourcenames.K8sGatewayFullname,
			"mesh",
		},
		Hosts: []string{
			"*." + domain,
			domain,
			clusterDomain,
		},
		Http: []v1alpha3.HTTPRoute{{
			Match: []v1alpha3.HTTPMatchRequest{{
				Authority: &v1alpha3.StringMatch{Exact: domain},
			}, {
				Authority: &v1alpha3.StringMatch{Exact: clusterDomain},
			}},
			Route: []v1alpha3.DestinationWeight{getActivatorDestinationWeight(100)},
			AppendHeaders: map[string]string{
				ctrl.GetRevisionHeaderName():        "test-rev",
				ctrl.GetRevisionHeaderNamespace():   testNamespace,
				resources.IstioTimeoutHackHeaderKey: resources.IstioTimeoutHackHeaderValue,
			},
			Timeout: resources.DefaultActivatorTimeout,
		}},
	}
	if diff := cmp.Diff(expectedSpec, vs.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestController(t)
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

	controller.Reconcile(KeyOrDie(route))

	vs, err := servingClient.NetworkingV1alpha3().VirtualServices(testNamespace).Get(resourcenames.VirtualService(route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting VirtualService: %v", err)
	}

	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	clusterDomain := "test-route.test.svc.cluster.local"
	expectedSpec := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress
		// Gateway, and the 'mesh' Gateway.  The former provides
		// access from outside of the cluster, and the latter provides
		// access for services from inside the cluster.
		Gateways: []string{
			resourcenames.K8sGatewayFullname,
			"mesh",
		},
		Hosts: []string{
			"*." + domain,
			domain,
			clusterDomain,
		},
		Http: []v1alpha3.HTTPRoute{{
			Match: []v1alpha3.HTTPMatchRequest{{
				Authority: &v1alpha3.StringMatch{Exact: domain},
			}, {
				Authority: &v1alpha3.StringMatch{Exact: clusterDomain},
			}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s-service.test.svc.cluster.local", cfgrev.Name),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 90,
			}, {
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s-service.test.svc.cluster.local", rev.Name),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 10,
			}},
		}},
	}
	if diff := cmp.Diff(expectedSpec, vs.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

// Test one out of multiple target revisions is in Reserve serving state.
func TestCreateRouteWithOneTargetReserve(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestController(t)
	// A standalone inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
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
			RevisionName: rev.Name,
			Percent:      10,
		}},
	)
	servingClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since Reconcile looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.Reconcile(KeyOrDie(route))

	vs, err := servingClient.NetworkingV1alpha3().VirtualServices(testNamespace).Get(resourcenames.VirtualService(route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting VirtualService: %v", err)
	}

	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	clusterDomain := "test-route.test.svc.cluster.local"
	expectedSpec := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress
		// Gateway, and the 'mesh' Gateway.  The former provides
		// access from outside of the cluster, and the latter provides
		// access for services from inside the cluster.
		Gateways: []string{
			resourcenames.K8sGatewayFullname,
			"mesh",
		},
		Hosts: []string{
			"*." + domain,
			domain,
			clusterDomain,
		},
		Http: []v1alpha3.HTTPRoute{{
			Match: []v1alpha3.HTTPMatchRequest{{
				Authority: &v1alpha3.StringMatch{Exact: domain},
			}, {
				Authority: &v1alpha3.StringMatch{Exact: clusterDomain},
			}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s-service.%s.svc.cluster.local", cfgrev.Name, testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 90,
			}, getActivatorDestinationWeight(10)},
			AppendHeaders: map[string]string{
				ctrl.GetRevisionHeaderName():        "test-rev",
				ctrl.GetRevisionHeaderNamespace():   testNamespace,
				resources.IstioTimeoutHackHeaderKey: resources.IstioTimeoutHackHeaderValue,
			},
			Timeout: resources.DefaultActivatorTimeout,
		}},
	}
	if diff := cmp.Diff(expectedSpec, vs.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestController(t)

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

	controller.Reconcile(KeyOrDie(route))

	vs, err := servingClient.NetworkingV1alpha3().VirtualServices(testNamespace).Get(resourcenames.VirtualService(route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting VirtualService: %v", err)
	}

	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	clusterDomain := "test-route.test.svc.cluster.local"
	expectedSpec := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress
		// Gateway, and the 'mesh' Gateway.  The former provides
		// access from outside of the cluster, and the latter provides
		// access for services from inside the cluster.
		Gateways: []string{
			resourcenames.K8sGatewayFullname,
			"mesh",
		},
		Hosts: []string{
			"*." + domain,
			domain,
			clusterDomain,
		},
		Http: []v1alpha3.HTTPRoute{{
			Match: []v1alpha3.HTTPMatchRequest{{
				Authority: &v1alpha3.StringMatch{Exact: domain},
			}, {
				Authority: &v1alpha3.StringMatch{Exact: clusterDomain},
			}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "p-deadbeef-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 50,
			}, {
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "test-rev-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 50,
			}},
		}, {
			Match: []v1alpha3.HTTPMatchRequest{{Authority: &v1alpha3.StringMatch{Exact: "test-revision-1." + domain}}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "test-rev-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 100,
			}},
		}, {
			Match: []v1alpha3.HTTPMatchRequest{{Authority: &v1alpha3.StringMatch{Exact: "test-revision-2." + domain}}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "test-rev-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 100,
			}},
		}},
	}
	if diff := cmp.Diff(expectedSpec, vs.Spec); diff != "" {
		fmt.Printf("%+v\n", vs.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithNamedTargets(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestController(t)
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

	controller.Reconcile(KeyOrDie(route))

	vs, err := servingClient.NetworkingV1alpha3().VirtualServices(testNamespace).Get(resourcenames.VirtualService(route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting virtualservice: %v", err)
	}
	domain := strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, ".")
	clusterDomain := "test-route.test.svc.cluster.local"
	expectedSpec := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress
		// Gateway, and the 'mesh' Gateway.  The former provides
		// access from outside of the cluster, and the latter provides
		// access for services from inside the cluster.
		Gateways: []string{
			resourcenames.K8sGatewayFullname,
			"mesh",
		},
		Hosts: []string{
			"*." + domain,
			domain,
			clusterDomain,
		},
		Http: []v1alpha3.HTTPRoute{{
			Match: []v1alpha3.HTTPMatchRequest{{
				Authority: &v1alpha3.StringMatch{Exact: domain},
			}, {
				Authority: &v1alpha3.StringMatch{Exact: clusterDomain},
			}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "test-rev-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 50,
			}, {
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "p-deadbeef-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 50,
			}},
		}, {
			Match: []v1alpha3.HTTPMatchRequest{{Authority: &v1alpha3.StringMatch{Exact: "bar." + domain}}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "p-deadbeef-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 100,
			}},
		}, {
			Match: []v1alpha3.HTTPMatchRequest{{Authority: &v1alpha3.StringMatch{Exact: "foo." + domain}}},
			Route: []v1alpha3.DestinationWeight{{
				Destination: v1alpha3.Destination{
					Host: fmt.Sprintf("%s.%s.svc.cluster.local", "test-rev-service", testNamespace),
					Port: v1alpha3.PortSelector{Number: 80},
				},
				Weight: 100,
			}},
		}},
	}
	if diff := cmp.Diff(expectedSpec, vs.Spec); diff != "" {
		fmt.Printf("%+v\n", vs.Spec)
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestEnqueueReferringRoute(t *testing.T) {
	_, servingClient, controller, _, servingInformer, _ := newTestController(t)
	routeClient := servingClient.ServingV1alpha1().Routes(testNamespace)

	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)

	routeClient.Create(route)
	// Since EnqueueReferringRoute looks in the lister, we need to add it to the informer
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// Update config to have LatestReadyRevisionName and route label.
	config.Status.LatestReadyRevisionName = rev.Name
	config.Labels = map[string]string{
		serving.RouteLabelKey: route.Name,
	}
	controller.EnqueueReferringRoute(config)
	// add this fake queue end marker.
	controller.WorkQueue.AddRateLimited("queue-has-no-work")
	expected := fmt.Sprintf("%s/%s", route.Namespace, route.Name)
	if k, _ := controller.WorkQueue.Get(); k != expected {
		t.Errorf("Expected %q, saw %q", expected, k)
	}
}

func TestEnqueueReferringRouteNotEnqueueIfCannotFindRoute(t *testing.T) {
	_, _, controller, _, _, _ := newTestController(t)

	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)

	// Update config to have LatestReadyRevisionName and route label.
	config.Status.LatestReadyRevisionName = rev.Name
	config.Labels = map[string]string{
		serving.RouteLabelKey: route.Name,
	}
	controller.EnqueueReferringRoute(config)
	// add this item to avoid being blocked by queue.
	expected := "queue-has-no-work"
	controller.WorkQueue.AddRateLimited(expected)
	if k, _ := controller.WorkQueue.Get(); k != expected {
		t.Errorf("Expected %v, saw %v", expected, k)
	}
}

func TestEnqueueReferringRouteNotEnqueueIfHasNoLatestReady(t *testing.T) {
	_, _, controller, _, _, _ := newTestController(t)
	config := getTestConfiguration()

	controller.EnqueueReferringRoute(config)
	// add this item to avoid being blocked by queue.
	expected := "queue-has-no-work"
	controller.WorkQueue.AddRateLimited(expected)
	if k, _ := controller.WorkQueue.Get(); k != expected {
		t.Errorf("Expected %v, saw %v", expected, k)
	}
}

func TestEnqueueReferringRouteNotEnqueueIfHavingNoRouteLabel(t *testing.T) {
	_, _, controller, _, _, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	fmt.Println(rev.Name)
	config.Status.LatestReadyRevisionName = rev.Name

	if controller.WorkQueue.Len() > 0 {
		t.Errorf("Expecting no route sync work prior to config change")
	}
	controller.EnqueueReferringRoute(config)
	// add this item to avoid being blocked by queue.
	expected := "queue-has-no-work"
	controller.WorkQueue.AddRateLimited(expected)
	if k, _ := controller.WorkQueue.Get(); k != expected {
		t.Errorf("Expected %v, saw %v", expected, k)
	}
}

func TestEnqueueReferringRouteNotEnqueueIfNotGivenAConfig(t *testing.T) {
	_, _, controller, _, _, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)

	if controller.WorkQueue.Len() > 0 {
		t.Errorf("Expecting no route sync work prior to config change")
	}
	controller.EnqueueReferringRoute(rev)
	// add this item to avoid being blocked by queue.
	expected := "queue-has-no-work"
	controller.WorkQueue.AddRateLimited(expected)
	if k, _ := controller.WorkQueue.Get(); k != expected {
		t.Errorf("Expected %v, saw %v", expected, k)
	}
}

func TestUpdateDomainConfigMap(t *testing.T) {
	kubeClient, servingClient, controller, kubeInformer, servingInformer, _ := newTestController(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := servingClient.ServingV1alpha1().Routes(route.Namespace)

	// Create a route.
	servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	routeClient.Create(route)
	controller.Reconcile(KeyOrDie(route))
	addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, route)

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
			controller.receiveDomainConfig(&domainConfig)
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
			controller.receiveDomainConfig(&domainConfig)
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
			controller.receiveDomainConfig(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}}
	for _, expectation := range expectations {
		expectation.apply()
		servingInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
		routeClient.Update(route)
		controller.Reconcile(KeyOrDie(route))
		addResourcesToInformers(t, kubeClient, kubeInformer, servingClient, servingInformer, route)

		route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
		expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, expectation.expectedDomainSuffix)
		if route.Status.Domain != expectedDomain {
			t.Errorf("Expected domain %q but saw %q", expectedDomain, route.Status.Domain)
		}
	}
}
