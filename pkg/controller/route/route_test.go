/*
Copyright 2018 Google LLC

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/istio/v1alpha2"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	ctrl "github.com/elafros/elafros/pkg/controller"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/elafros/elafros/pkg/controller/testing"
)

const testNamespace string = "test"

func getTestRouteWithTrafficTargets(traffic []v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/Routes/test-route",
			Name:      "test-route",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
}

func getTestRevision(name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/test/revisions/%s", name),
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Container: &corev1.Container{
				Image: "test-image",
			},
		},
		Status: v1alpha1.RevisionStatus{
			ServiceName: fmt.Sprintf("%s-service", name),
		},
	}
}

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisiontemplates/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			// This is a workaround for generation initialization
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: &corev1.Container{
						Image: "test-image",
					},
				},
			},
		},
	}
}

func getTestRevisionForConfig(config *v1alpha1.Configuration) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/p-deadbeef",
			Name:      "p-deadbeef",
			Namespace: testNamespace,
			Labels: map[string]string{
				ela.ConfigurationLabelKey: config.Name,
			},
		},
		Spec: *config.Spec.RevisionTemplate.Spec.DeepCopy(),
		Status: v1alpha1.RevisionStatus{
			ServiceName: "p-deadbeef-service",
		},
	}
}

func newTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	controller = NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
		ctrl.Config{DomainSuffix: "test-domain.net"},
	).(*Controller)

	return
}

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, elaClient, controller, kubeInformer, elaInformer = newTestController(t, elaObjects...)

	// Start the informers. This must happen after the call to NewController,
	// otherwise there are no informers to be started.
	stopCh = make(chan struct{})
	kubeInformer.Start(stopCh)
	elaInformer.Start(stopCh)

	// Run the controller.
	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	return
}

func TestCreateRouteCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)

	h := NewHooks()
	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	h.OnCreate(&kubeClient.Fake, "events", ExpectEventDelivery(t, `Created service "test-route-service"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectEventDelivery(t, `Created Ingress "test-route-ela-ingress"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectEventDelivery(t, `Created Istio route rule "test-route-istio"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectEventDelivery(t, `Updated status for route "test-route"`))

	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      100,
			},
		},
	)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	// Look for the placeholder service.
	expectedServiceName := fmt.Sprintf("%s-service", route.Name)
	service, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedServiceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting service: %v", err)
	}

	expectedPorts := []corev1.ServicePort{
		corev1.ServicePort{
			Name: "http",
			Port: 80,
		},
	}

	if diff := cmp.Diff(expectedPorts, service.Spec.Ports); diff != "" {
		t.Errorf("Unexpected service ports diff (-want +got): %v", diff)
	}

	// Look for the ingress.
	expectedIngressName := fmt.Sprintf("%s-ela-ingress", route.Name)
	ingress, err := kubeClient.ExtensionsV1beta1().Ingresses(testNamespace).Get(expectedIngressName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting ingress: %v", err)
	}

	expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
	expectedWildcardDomainPrefix := fmt.Sprintf("*.%s", expectedDomainPrefix)
	if !strings.HasPrefix(ingress.Spec.Rules[0].Host, expectedDomainPrefix) {
		t.Errorf("Ingress host '%s' does not have prefix '%s'", ingress.Spec.Rules[0].Host, expectedDomainPrefix)
	}
	if !strings.HasPrefix(ingress.Spec.Rules[1].Host, expectedWildcardDomainPrefix) {
		t.Errorf("Ingress host '%s' does not have prefix '%s'", ingress.Spec.Rules[1].Host, expectedWildcardDomainPrefix)
	}

	// Look for the route rule.
	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	// Check labels
	expectedLabels := map[string]string{"route": route.Name}
	if diff := cmp.Diff(expectedLabels, routerule.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	// Check owner refs
	expectedRefs := []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: "elafros.dev/v1alpha1",
			Kind:       "Route",
			Name:       route.Name,
		},
	}

	if diff := cmp.Diff(expectedRefs, routerule.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: "test-route-service",
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, controller.controllerConfig.DomainSuffix),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 100,
			},
		},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(cfgrev)

	// A route targeting both the config and standalone revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           90,
			},
			v1alpha1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      10,
			},
		},
	)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: fmt.Sprintf("%s-service", route.Name),
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, controller.controllerConfig.DomainSuffix),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: fmt.Sprintf("%s-service.test", cfgrev.Name),
				},
				Weight: 90,
			},
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: fmt.Sprintf("%s-service.test", rev.Name),
				},
				Weight: 10,
			},
		},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}

}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(cfgrev)

	// A route with duplicate targets. These will be deduped.
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: "test-config",
				Percent:           30,
			},
			v1alpha1.TrafficTarget{
				ConfigurationName: "test-config",
				Percent:           20,
			},
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      10,
			},
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      5,
			},
			v1alpha1.TrafficTarget{
				Name:         "test-revision-1",
				RevisionName: "test-rev",
				Percent:      10,
			},
			v1alpha1.TrafficTarget{
				Name:         "test-revision-1",
				RevisionName: "test-rev",
				Percent:      10,
			},
			v1alpha1.TrafficTarget{
				Name:         "test-revision-2",
				RevisionName: "test-rev",
				Percent:      15,
			},
		},
	)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: "test-route-service",
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, controller.controllerConfig.DomainSuffix),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "p-deadbeef-service.test",
				},
				Weight: 50,
			},
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 15,
			},
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 20,
			},
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 15,
			},
		},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithNamedTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(cfgrev)

	// A route targeting both the config and standalone revision with named
	// targets
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Name:         "foo",
				RevisionName: "test-rev",
				Percent:      50,
			},
			v1alpha1.TrafficTarget{
				Name:              "bar",
				ConfigurationName: "test-config",
				Percent:           50,
			},
		},
	)

	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	domain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, controller.controllerConfig.DomainSuffix)

	expectRouteSpec := func(t *testing.T, name string, expectedSpec v1alpha2.RouteRuleSpec) {
		routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("error getting routerule: %v", err)
		}
		if diff := cmp.Diff(expectedSpec, routerule.Spec); diff != "" {
			t.Errorf("Unexpected routerule spec diff (-want +got): %v", diff)
		}
	}

	// Expects authority header to be the domain suffix
	expectRouteSpec(t, "test-route-istio", v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: "test-route-service",
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: domain,
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 50,
			},
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "p-deadbeef-service.test",
				},
				Weight: 50,
			},
		},
	})

	// Expects authority header to have the traffic target name prefixed to the
	// domain suffix. Also weights 100% of the traffic to the specified traffic
	// target's revision.
	expectRouteSpec(t, "test-route-foo-istio", v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: "test-route-service",
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: fmt.Sprintf("foo.%s", domain),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "test-rev-service.test",
				},
				Weight: 100,
			},
		},
	})

	// Expects authority header to have the traffic target name prefixed to the
	// domain suffix. Also weights 100% of the traffic to the specified traffic
	// target's revision.
	expectRouteSpec(t, "test-route-bar-istio", v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name: "test-route-service",
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: fmt.Sprintf("bar.%s", domain),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{
			v1alpha2.DestinationWeight{
				Destination: v1alpha2.IstioService{
					Name: "p-deadbeef-service.test",
				},
				Weight: 100,
			},
		},
	})
}

func TestSetLabelToConfigurationDirectlyConfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           100,
			},
		},
	)

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ElafrosV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Configuration should be labeled for this route
	expectedLabels := map[string]string{ela.RouteLabelKey: route.Name}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}
}

func TestSetLabelToConfigurationIndirectlyConfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      100,
			},
		},
	)

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ElafrosV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Configuration should be labeled for this route
	expectedLabels := map[string]string{ela.RouteLabelKey: route.Name}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithInvalidConfigurationShouldReturnError(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      100,
			},
		},
	)
	// Set config's route label with another route name to trigger an error.
	config.Labels = map[string]string{ela.RouteLabelKey: "another-route"}

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// No configuration updates.
	elaClient.Fake.PrependReactor("update", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Configuration was updated unexpectedly")
			return true, nil, nil
		},
	)

	// No route updates.
	elaClient.Fake.PrependReactor("update", "route",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Route was updated unexpectedly")
			return true, nil, nil
		},
	)

	expectedErrMsg := "Configuration \"test-config\" is already in use by \"another-route\", and cannot be used by \"test-route\""
	// Should return error.
	err := controller.updateRouteEvent(route.Namespace + "/" + route.Name)
	if wanted, got := expectedErrMsg, err.Error(); wanted != got {
		t.Errorf("unexpected error: %q expected: %q", got, wanted)
	}
}

func TestSetLabelNotChangeConfigurationLabelIfLabelExists(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      100,
			},
		},
	)
	// Set config's route label with route name to make sure config's label will not be set
	// by function setLabelForGivenConfigurations.
	config.Labels = map[string]string{ela.RouteLabelKey: route.Name}

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// No configuration updates
	elaClient.Fake.PrependReactor("update", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Configuration was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.updateRouteEvent(route.Namespace + "/" + route.Name)
}

func TestDeleteLabelOfConfigurationWhenUnconfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	config := getTestConfiguration()
	// Set a label which is expected to be deleted.
	config.Labels = map[string]string{ela.RouteLabelKey: route.Name}
	rev := getTestRevisionForConfig(config)

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ElafrosV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Check labels, should be empty.
	expectedLabels := map[string]string{}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}
}

func TestUpdateRouteWhenConfigurationChanges(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	routeClient := elaClient.ElafrosV1alpha1().Routes(testNamespace)

	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           100,
			},
		},
	)

	routeClient.Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	// Since addConfigurationEvent looks in the lister, we need to add it to the
	// informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)

	controller.addConfigurationEvent(config)

	route, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get route: %v", err)
	}

	// The configuration has no LatestReadyRevisionName, so there should be no
	// routed targets.
	var expectedTrafficTargets []v1alpha1.TrafficTarget
	if diff := cmp.Diff(expectedTrafficTargets, route.Status.Traffic); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	// Update config to have LatestReadyRevisionName and route label.
	config.Status.LatestReadyRevisionName = rev.Name
	config.Labels = map[string]string{
		ela.RouteLabelKey: route.Name,
	}

	// We need to update the config in the client since getDirectTrafficTargets
	// gets the configuration from there
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Update(config)
	// Since addConfigurationEvent looks in the lister, we need to add it to the
	// informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	controller.addConfigurationEvent(config)

	route, err = routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get route: %v", err)
	}

	// Now the configuration has a LatestReadyRevisionName, so its revision should
	// be targeted
	expectedTrafficTargets = []v1alpha1.TrafficTarget{
		v1alpha1.TrafficTarget{
			RevisionName: rev.Name,
			Percent:      100,
		},
	}
	if diff := cmp.Diff(expectedTrafficTargets, route.Status.Traffic); diff != "" {
		t.Errorf("Unexpected traffic target diff (-want +got): %v", diff)
	}
	expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
	if !strings.HasPrefix(route.Status.Domain, expectedDomainPrefix) {
		t.Errorf("Route domain '%s' must have prefix '%s'", route.Status.Domain, expectedDomainPrefix)
	}

}

func TestAddConfigurationEventNotUpdateAnythingIfHasNoLatestReady(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: config.Name,
				Percent:           100,
			},
		},
	)
	// If set config.Status.LatestReadyRevisionName = rev.Name, the test should fail.
	config.Status.LatestCreatedRevisionName = rev.Name
	config.Labels = map[string]string{ela.RouteLabelKey: route.Name}

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ElafrosV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// No configuration updates
	elaClient.Fake.PrependReactor("update", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Configuration was updated unexpectedly")
			return true, nil, nil
		},
	)

	// No route updates
	elaClient.Fake.PrependReactor("update", "routes",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Route was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addConfigurationEvent(config)
}

func TestUpdateIngressEventUpdateRouteStatus(t *testing.T) {
	kubeClient, elaClient, controller, _, _ := newTestController(t)

	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			{
				RevisionName: "test-rev",
				Percent:      100,
			},
		},
	)
	// Create a route.
	routeClient := elaClient.ElafrosV1alpha1().Routes(route.Namespace)
	routeClient.Create(route)
	// Create an ingress owned by this route.
	controller.reconcileIngress(route)
	// Before ingress has an IP address, route isn't marked as Ready.
	ingressClient := kubeClient.Extensions().Ingresses(route.Namespace)
	ingress, _ := ingressClient.Get(ctrl.GetElaK8SIngressName(route), metav1.GetOptions{})
	controller.updateIngressEvent(nil, ingress)
	route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
	if nil != route.Status.Conditions {
		t.Errorf("Route Status.Conditions should be nil, saw %v", route.Status.Conditions)
	}
	// Update the Ingress IP.
	ingress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{
		IP: "127.0.0.1",
	}}
	controller.updateIngressEvent(nil, ingress)
	// Verify now that Route.Status.Conditions is set correctly.
	expectedConditions := []v1alpha1.RouteCondition{{
		Type:   v1alpha1.RouteConditionReady,
		Status: corev1.ConditionTrue,
	}}
	newRoute, _ := routeClient.Get(route.Name, metav1.GetOptions{})
	if diff := cmp.Diff(expectedConditions, newRoute.Status.Conditions); diff != "" {
		t.Errorf("Unexpected condition diff (-want +got): %v", diff)
	}
}
