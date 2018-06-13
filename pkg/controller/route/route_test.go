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
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/knative/serving/pkg"

	"go.uber.org/zap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/apis/istio/v1alpha2"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	ctrl "github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/controller/testing"
)

const (
	testNamespace       string = "test"
	defaultDomainSuffix string = "test-domain.dev"
	prodDomainSuffix    string = "prod-domain.com"
)

var (
	testLogger = zap.NewNop().Sugar()
	testCtx    = logging.WithLogger(context.Background(), testLogger)
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
	return &v1alpha1.Revision{
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
}

func getActivatorDestinationWeight(w int) v1alpha2.DestinationWeight {
	return v1alpha2.DestinationWeight{
		Destination: v1alpha2.IstioService{
			Name:      ctrl.GetElaK8SActivatorServiceName(),
			Namespace: pkg.GetServingSystemNamespace(),
		},
		Weight: w,
	}
}

func newTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	servingSystemInformer kubeinformers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	domainConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ctrl.GetDomainConfigMapName(),
			Namespace: pkg.GetServingSystemNamespace(),
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			prodDomainSuffix:    "selector:\n  app: prod",
		},
	}
	kubeClient.Core().ConfigMaps(pkg.GetServingSystemNamespace()).Create(&domainConfig)
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)
	servingSystemInformer = kubeinformers.NewFilteredSharedInformerFactory(kubeClient, 0, pkg.GetServingSystemNamespace(), nil)

	controller = NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		servingSystemInformer,
		&rest.Config{},
		k8sflag.Bool("enable-scale-to-zero", false),
		testLogger,
	).(*Controller)

	return
}

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	servingSystemInformer kubeinformers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, elaClient, controller, kubeInformer, elaInformer, servingSystemInformer = newTestController(t, elaObjects...)

	// Start the informers. This must happen after the call to NewController,
	// otherwise there are no informers to be started.
	stopCh = make(chan struct{})
	kubeInformer.Start(stopCh)
	elaInformer.Start(stopCh)
	servingSystemInformer.Start(stopCh)

	// Run the controller.
	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	return
}

func TestCreateRouteCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer, _ := newTestController(t)

	h := NewHooks()
	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created service "test-route-service"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created Ingress "test-route-ingress"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created Istio route rule "test-route-istio"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Updated status for route "test-route"`))

	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: "test-rev",
			Percent:      100,
		}},
	)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	// Look for the placeholder service.
	expectedServiceName := fmt.Sprintf("%s-service", route.Name)
	service, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedServiceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting service: %v", err)
	}

	expectedPorts := []corev1.ServicePort{{
		Name: "http",
		Port: 80,
	}}

	if diff := cmp.Diff(expectedPorts, service.Spec.Ports); diff != "" {
		t.Errorf("Unexpected service ports diff (-want +got): %v", diff)
	}

	// Look for the ingress.
	expectedIngressName := fmt.Sprintf("%s-ingress", route.Name)
	ingress, err := kubeClient.ExtensionsV1beta1().Ingresses(testNamespace).Get(expectedIngressName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("error getting ingress: %v", err)
	}

	expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
	expectedWildcardDomainPrefix := fmt.Sprintf("*.%s", expectedDomainPrefix)
	if !strings.HasPrefix(ingress.Spec.Rules[0].Host, expectedDomainPrefix) {
		t.Errorf("Ingress host %q does not have prefix %q", ingress.Spec.Rules[0].Host, expectedDomainPrefix)
	}
	if !strings.HasPrefix(ingress.Spec.Rules[1].Host, expectedWildcardDomainPrefix) {
		t.Errorf("Ingress host %q does not have prefix %q", ingress.Spec.Rules[1].Host, expectedWildcardDomainPrefix)
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
	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Route",
		Name:       route.Name,
	}}

	if diff := cmp.Diff(expectedRefs, routerule.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 100,
		}, getActivatorDestinationWeight(0)},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

// Test the only revision in the route is in Reserve (inactive) serving status.
func TestCreateRouteForOneReserveRevision(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer, _ := newTestController(t)
	controller.enableScaleToZero = k8sflag.Bool("enable-scale-to-zero", true)

	h := NewHooks()
	// Look for the events. Events are delivered asynchronously so we need to use
	// hooks here. Each hook tests for a specific event.
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created service "test-route-service"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created Ingress "test-route-ingress"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Created Istio route rule "test-route-istio"`))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, `Updated status for route "test-route"`))

	// An inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
		})
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: "test-rev",
			Percent:      100,
		}},
	)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	// Look for the route rule with activator as the destination.
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
	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Route",
		Name:       route.Name,
	}}

	if diff := cmp.Diff(expectedRefs, routerule.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
	}

	appendHeaders := make(map[string]string)
	appendHeaders[ctrl.GetRevisionHeaderName()] = "test-rev"
	appendHeaders[ctrl.GetRevisionHeaderNamespace()] = testNamespace
	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route:         []v1alpha2.DestinationWeight{getActivatorDestinationWeight(100)},
		AppendHeaders: appendHeaders,
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteFromConfigsWithMultipleRevs(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	latestReadyRev := getTestRevisionForConfig(config)
	otherRev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisions/p-livebeef",
			Name:      "p-livebeef",
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: config.Name,
			},
		},
		Spec: *config.Spec.RevisionTemplate.Spec.DeepCopy(),
		Status: v1alpha1.RevisionStatus{
			ServiceName: "p-livebeef-service",
		},
	}
	config.Status.LatestReadyRevisionName = latestReadyRev.Name
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(latestReadyRev)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(otherRev)

	// A route targeting both the config and standalone revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      fmt.Sprintf("%s-service", route.Name),
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      fmt.Sprintf("%s-service", latestReadyRev.Name),
				Namespace: testNamespace,
			},
			Weight: 100,
		}, getActivatorDestinationWeight(0),
			{
				Destination: v1alpha2.IstioService{
					Name:      fmt.Sprintf("%s-service", otherRev.Name),
					Namespace: testNamespace,
				},
				Weight: 0,
			}},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)

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
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      fmt.Sprintf("%s-service", route.Name),
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      fmt.Sprintf("%s-service", cfgrev.Name),
				Namespace: testNamespace,
			},
			Weight: 90,
		}, {
			Destination: v1alpha2.IstioService{
				Name:      fmt.Sprintf("%s-service", rev.Name),
				Namespace: testNamespace,
			},
			Weight: 10,
		}, getActivatorDestinationWeight(0)},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}

}

// Test one out of multiple target revisions is in Reserve serving state.
func TestCreateRouteWithOneTargetReserve(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	controller.enableScaleToZero = k8sflag.Bool("enable-scale-to-zero", true)

	// A standalone inactive revision
	rev := getTestRevisionWithCondition("test-rev",
		v1alpha1.RevisionCondition{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionFalse,
			Reason: "Inactive",
		})
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)

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
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	appendHeaders := make(map[string]string)
	appendHeaders[ctrl.GetRevisionHeaderName()] = "test-rev"
	appendHeaders[ctrl.GetRevisionHeaderNamespace()] = testNamespace
	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      fmt.Sprintf("%s-service", route.Name),
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      fmt.Sprintf("%s-service", cfgrev.Name),
				Namespace: testNamespace,
			},
			Weight: 90,
		}, getActivatorDestinationWeight(10)},
		AppendHeaders: appendHeaders,
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}

}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)

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
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	routerule, err := elaClient.ConfigV1alpha2().RouteRules(testNamespace).Get(fmt.Sprintf("%s-istio", route.Name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting routerule: %v", err)
	}

	expectedRouteSpec := v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{route.Name, route.Namespace, defaultDomainSuffix}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      "p-deadbeef-service",
				Namespace: testNamespace,
			},
			Weight: 50,
		}, {
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 15,
		}, {
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 20,
		}, {
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 15,
		},
			getActivatorDestinationWeight(0)},
	}

	if diff := cmp.Diff(expectedRouteSpec, routerule.Spec); diff != "" {
		t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithNamedTargets(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	// A configuration and associated revision. Normally the revision would be
	// created by the configuration controller.
	config := getTestConfiguration()
	cfgrev := getTestRevisionForConfig(config)
	config.Status.LatestReadyRevisionName = cfgrev.Name
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(cfgrev)

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

	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	domain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, defaultDomainSuffix)

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
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(domain),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 50,
		}, {
			Destination: v1alpha2.IstioService{
				Name:      "p-deadbeef-service",
				Namespace: testNamespace,
			},
			Weight: 50,
		}, getActivatorDestinationWeight(0)},
	})

	// Expects authority header to have the traffic target name prefixed to the
	// domain suffix. Also weights 100% of the traffic to the specified traffic
	// target's revision.
	expectRouteSpec(t, "test-route-foo-istio", v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{"foo", domain}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      "test-rev-service",
				Namespace: testNamespace,
			},
			Weight: 100,
		}},
	})

	// Expects authority header to have the traffic target name prefixed to the
	// domain suffix. Also weights 100% of the traffic to the specified traffic
	// target's revision.
	expectRouteSpec(t, "test-route-bar-istio", v1alpha2.RouteRuleSpec{
		Destination: v1alpha2.IstioService{
			Name:      "test-route-service",
			Namespace: testNamespace,
		},
		Match: v1alpha2.Match{
			Request: v1alpha2.MatchRequest{
				Headers: v1alpha2.Headers{
					Authority: v1alpha2.MatchString{
						Regex: regexp.QuoteMeta(
							strings.Join([]string{"bar", domain}, "."),
						),
					},
				},
			},
		},
		Route: []v1alpha2.DestinationWeight{{
			Destination: v1alpha2.IstioService{
				Name:      "p-deadbeef-service",
				Namespace: testNamespace,
			},
			Weight: 100,
		}},
	})
}

func TestCreateRouteDeletesOutdatedRouteRules(t *testing.T) {
	_, elaClient, controller, _, _, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           50,
		}, {
			ConfigurationName: config.Name,
			Percent:           100,
			Name:              "foo",
		}},
	)
	extraRouteRule := &v1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route-extra-istio",
			Namespace: route.Namespace,
			Labels: map[string]string{
				"route": route.Name,
			},
		},
	}

	// A route rule without the expected ela route label.
	independentRouteRule := &v1alpha2.RouteRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route-independent-istio",
			Namespace: route.Namespace,
		},
	}

	config.Status.LatestCreatedRevisionName = rev.Name
	config.Labels = map[string]string{serving.RouteLabelKey: route.Name}

	elaClient.ServingV1alpha1().Configurations("test").Create(config)
	elaClient.ServingV1alpha1().Revisions("test").Create(rev)
	elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Create(extraRouteRule)
	elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Create(independentRouteRule)

	// Ensure extraRouteRule was created
	if _, err := elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Get(extraRouteRule.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Unexpected error occured. Expected route rule %s to exist.", extraRouteRule.Name)
	}
	if _, err := elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Get(independentRouteRule.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Unexpected error occured. Expected route rule %s to exist.", independentRouteRule.Name)
	}
	elaClient.ServingV1alpha1().Routes("test").Create(route)

	if err := controller.removeOutdatedRouteRules(testCtx, route); err != nil {
		t.Errorf("Unexpected error occurred removing outdated route rules: %s", err)
	}

	expectedErrMsg := fmt.Sprintf("routerules.config.istio.io \"%s\" not found", extraRouteRule.Name)
	// expect extraRouteRule to have been deleted
	_, err := elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Get(extraRouteRule.Name, metav1.GetOptions{})
	if wanted, got := expectedErrMsg, err.Error(); wanted != got {
		t.Errorf("Unexpected error: %q expected: %q", got, wanted)
	}
	// expect independentRouteRule not to have been deleted
	if _, err := elaClient.ConfigV1alpha2().RouteRules(route.Namespace).Get(independentRouteRule.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Error occurred fetching route rule: %s. Expected route rule to exist.", independentRouteRule.Name)
	}
}

func TestSetLabelToConfigurationDirectlyConfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ServingV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Configuration should be labeled for this route
	expectedLabels := map[string]string{serving.RouteLabelKey: route.Name}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}
}

func TestSetLabelToRevisionDirectlyConfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	controller.updateRouteEvent(KeyOrDie(route))

	rev, err := elaClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting revision: %v", err)
	}

	// Revision should not have route label as the revision is not marked as the Config's latest ready revision
	expectedLabels := map[string]string{
		serving.ConfigurationLabelKey: config.Name,
	}

	if diff := cmp.Diff(expectedLabels, rev.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}

	// Mark the revision as the Config's latest ready revision
	config.Status.LatestReadyRevisionName = rev.Name

	elaClient.ServingV1alpha1().Configurations(testNamespace).Update(config)
	controller.updateRouteEvent(KeyOrDie(route))

	rev, err = elaClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting revision: %v", err)
	}

	// Revision should have the route label
	expectedLabels = map[string]string{
		serving.ConfigurationLabelKey: config.Name,
		serving.RouteLabelKey:         route.Name,
	}

	if diff := cmp.Diff(expectedLabels, rev.Labels); diff != "" {
		t.Errorf("Unexpected label diff (-want +got): %v", diff)
	}
}

func TestSetLabelToConfigurationAndRevisionIndirectlyConfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: rev.Name,
			Percent:      100,
		}},
	)

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ServingV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Configuration should be labeled for this route
	expectedLabels := map[string]string{serving.RouteLabelKey: route.Name}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label in configuration diff (-want +got): %v", diff)
	}

	rev, err = elaClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting revision: %v", err)
	}

	// Revision should have the route label
	expectedLabels = map[string]string{
		serving.ConfigurationLabelKey: config.Name,
		serving.RouteLabelKey:         route.Name,
	}

	if diff := cmp.Diff(expectedLabels, rev.Labels); diff != "" {
		t.Errorf("Unexpected label in revision diff (-want +got): %v", diff)
	}
}

func TestCreateRouteWithInvalidConfigurationShouldReturnError(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: rev.Name,
			Percent:      100,
		}},
	)
	// Set config's route label with another route name to trigger an error.
	config.Labels = map[string]string{serving.RouteLabelKey: "another-route"}

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

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

// Helper to compare RouteConditions
func sortConditions(a, b v1alpha1.RouteCondition) bool {
	return a.Type < b.Type
}

func TestCreateRouteRevisionMissingCondition(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			// Note that since no Revision with this name exists,
			// this will trigger the RevisionMissing condition.
			RevisionName: "does-not-exist",
			Percent:      100,
		}},
	)

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	expectedErrMsg := `revisions.serving.knative.dev "does-not-exist" not found`
	// Should return error.
	err := controller.updateRouteEvent(route.Namespace + "/" + route.Name)
	if wanted, got := expectedErrMsg, err.Error(); wanted != got {
		t.Errorf("unexpected error: %q expected: %q", got, wanted)
	}

	// Verify that Route.Status.Conditions were correctly set.
	newRoute, _ := elaClient.ServingV1alpha1().Routes(route.Namespace).Get(route.Name, metav1.GetOptions{})

	for _, ct := range []v1alpha1.RouteConditionType{"AllTrafficAssigned", "Ready"} {
		got := newRoute.Status.GetCondition(ct)
		want := &v1alpha1.RouteCondition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionMissing",
			Message:            `Referenced Revision "does-not-exist" not found`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}
}

func TestCreateRouteConfigurationMissingCondition(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			// Note that since no Configuration with this name exists,
			// this will trigger the ConfigurationMissing condition.
			ConfigurationName: "does-not-exist",
			Percent:           100,
		}},
	)

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	expectedErrMsg := `configurations.serving.knative.dev "does-not-exist" not found`
	// Should return error.
	err := controller.updateRouteEvent(route.Namespace + "/" + route.Name)
	if wanted, got := expectedErrMsg, err.Error(); wanted != got {
		t.Errorf("unexpected error: %q expected: %q", got, wanted)
	}

	// Verify that Route.Status.Conditions were correctly set.
	newRoute, _ := elaClient.ServingV1alpha1().Routes(route.Namespace).Get(route.Name, metav1.GetOptions{})

	for _, ct := range []v1alpha1.RouteConditionType{"AllTrafficAssigned", "Ready"} {
		got := newRoute.Status.GetCondition(ct)
		want := &v1alpha1.RouteCondition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "ConfigurationMissing",
			Message:            `Referenced Configuration "does-not-exist" not found`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}
}

func TestSetLabelNotChangeConfigurationAndRevisionLabelIfLabelExists(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: rev.Name,
			Percent:      100,
		}},
	)
	// Set config's route label with route name to make sure config's label will not be set
	// by function setLabelForGivenConfigurations.
	config.Labels = map[string]string{serving.RouteLabelKey: route.Name}

	// Set revision's route label with route name to make sure revision's label will not be set
	// by function setLabelForGivenRevisions.
	rev.Labels[serving.RouteLabelKey] = route.Name

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// Assert that the configuration is not updated when updateRouteEvent is called.
	elaClient.Fake.PrependReactor("update", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Configuration was updated unexpectedly")
			return true, nil, nil
		},
	)

	// Assert that the revision is not updated when updateRouteEvent is called.
	elaClient.Fake.PrependReactor("update", "revision",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.updateRouteEvent(route.Namespace + "/" + route.Name)
}

func TestDeleteLabelOfConfigurationAndRevisionWhenUnconfigured(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	config := getTestConfiguration()
	// Set a route label in configuration which is expected to be deleted.
	config.Labels = map[string]string{serving.RouteLabelKey: route.Name}
	rev := getTestRevisionForConfig(config)
	// Set a route label in revision which is expected to be deleted.
	rev.Labels[serving.RouteLabelKey] = route.Name

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	controller.updateRouteEvent(KeyOrDie(route))

	config, err := elaClient.ServingV1alpha1().Configurations(testNamespace).Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	// Check labels in configuration, should be empty.
	expectedLabels := map[string]string{}
	if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
		t.Errorf("Unexpected label in Configuration diff (-want +got): %v", diff)
	}

	rev, err = elaClient.ServingV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting revision: %v", err)
	}

	expectedLabels = map[string]string{
		serving.ConfigurationLabelKey: config.Name,
	}

	// Check labels in revision, should be empty.
	if diff := cmp.Diff(expectedLabels, rev.Labels); diff != "" {
		t.Errorf("Unexpected label in revision diff (-want +got): %v", diff)
	}

}

func TestUpdateRouteDomainWhenRouteLabelChanges(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer, _ := newTestController(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := elaClient.ServingV1alpha1().Routes(route.Namespace)
	ingressClient := kubeClient.ExtensionsV1beta1().Ingresses(route.Namespace)

	// Create a route.
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	routeClient.Create(route)
	controller.updateRouteEvent(KeyOrDie(route))

	// Confirms that by default the route get the defaultDomainSuffix.
	expectations := []struct {
		labels       map[string]string
		domainSuffix string
	}{
		{labels: map[string]string{}, domainSuffix: defaultDomainSuffix},
		{labels: map[string]string{"app": "notprod"}, domainSuffix: defaultDomainSuffix},
		{labels: map[string]string{"unrelated": "unrelated"}, domainSuffix: defaultDomainSuffix},
		{labels: map[string]string{"app": "prod"}, domainSuffix: prodDomainSuffix},
		{labels: map[string]string{"app": "prod", "unrelated": "whocares"}, domainSuffix: prodDomainSuffix},
	}
	for _, expectation := range expectations {
		route.ObjectMeta.Labels = expectation.labels
		elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
		routeClient.Update(route)
		controller.updateRouteEvent(KeyOrDie(route))

		// Confirms that the new route get the correct domain.
		route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
		expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, expectation.domainSuffix)
		if route.Status.Domain != expectedDomain {
			t.Errorf("For labels %v, expected domain %q but saw %q", expectation.labels, expectedDomain, route.Status.Domain)
		}

		// Confirms that the ingress is updated in tandem with the route.
		ingress, _ := ingressClient.Get(ctrl.GetElaK8SIngressName(route), metav1.GetOptions{})

		expectedHost := route.Status.Domain
		expectedWildcardHost := fmt.Sprintf("*.%s", route.Status.Domain)
		if ingress.Spec.Rules[0].Host != expectedHost {
			t.Errorf("For labels %v, expected ingress host %q but saw %q", expectation.labels, expectedHost, ingress.Spec.Rules[0].Host)
		}
		if ingress.Spec.Rules[1].Host != expectedWildcardHost {
			t.Errorf("For labels %v, expected ingress host %q but saw %q", expectation.labels, expectedWildcardHost, ingress.Spec.Rules[1].Host)
		}
	}
}

func TestUpdateRouteWhenConfigurationChanges(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	routeClient := elaClient.ServingV1alpha1().Routes(testNamespace)

	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)

	routeClient.Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since SyncConfiguration looks in the lister, we need to add it to the
	// informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	controller.SyncConfiguration(config)

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
		serving.RouteLabelKey: route.Name,
	}

	// We need to update the config in the client since getDirectTrafficTargets
	// gets the configuration from there
	elaClient.ServingV1alpha1().Configurations(testNamespace).Update(config)
	// Since SyncConfiguration looks in the lister, we need to add it to the
	// informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	controller.SyncConfiguration(config)

	route, err = routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get route: %v", err)
	}

	// Now the configuration has a LatestReadyRevisionName, so its revision should
	// be targeted
	expectedTrafficTargets = []v1alpha1.TrafficTarget{{
		RevisionName: rev.Name,
		Percent:      100,
	}, {
		Name:    ctrl.GetElaK8SActivatorServiceName(),
		Percent: 0,
	}}
	if diff := cmp.Diff(expectedTrafficTargets, route.Status.Traffic); diff != "" {
		t.Errorf("Unexpected traffic target diff (-want +got): %v", diff)
	}
	expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
	if !strings.HasPrefix(route.Status.Domain, expectedDomainPrefix) {
		t.Errorf("Route domain %q must have prefix %q", route.Status.Domain, expectedDomainPrefix)
	}

}

func TestAddConfigurationEventNotUpdateAnythingIfHasNoLatestReady(t *testing.T) {
	_, elaClient, controller, _, elaInformer, _ := newTestController(t)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			ConfigurationName: config.Name,
			Percent:           100,
		}},
	)
	// If set config.Status.LatestReadyRevisionName = rev.Name, the test should fail.
	config.Status.LatestCreatedRevisionName = rev.Name
	config.Labels = map[string]string{serving.RouteLabelKey: route.Name}

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	elaClient.ServingV1alpha1().Routes(testNamespace).Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

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

	controller.SyncConfiguration(config)
}

// Test route when we do not use activator, and then use activator.
func TestUpdateIngressEventUpdateRouteStatus(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer, _ := newTestController(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	elaClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)

	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: rev.Name,
			Percent:      100,
		}},
	)
	// Create a route.
	routeClient := elaClient.ServingV1alpha1().Routes(route.Namespace)
	routeClient.Create(route)
	// Since updateRouteEvent looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	controller.updateRouteEvent(KeyOrDie(route))

	// Before ingress has an IP address, route isn't marked as Ready.
	ingressClient := kubeClient.ExtensionsV1beta1().Ingresses(route.Namespace)
	ingress, _ := ingressClient.Get(ctrl.GetElaK8SIngressName(route), metav1.GetOptions{})
	controller.SyncIngress(ingress)

	newRoute, _ := routeClient.Get(route.Name, metav1.GetOptions{})
	for _, ct := range []v1alpha1.RouteConditionType{"Ready"} {
		got := newRoute.Status.GetCondition(ct)
		want := &v1alpha1.RouteCondition{
			Type:               ct,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}

	// Update the Ingress IP.
	ingress.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{
		IP: "127.0.0.1",
	}}
	controller.SyncIngress(ingress)

	// Verify now that Route.Status.Conditions is set correctly.
	newRoute, _ = routeClient.Get(route.Name, metav1.GetOptions{})
	for _, ct := range []v1alpha1.RouteConditionType{"Ready"} {
		got := newRoute.Status.GetCondition(ct)
		want := &v1alpha1.RouteCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}
}

func TestUpdateDomainConfigMap(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer, _ := newTestController(t)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	routeClient := elaClient.ServingV1alpha1().Routes(route.Namespace)
	ingressClient := kubeClient.Extensions().Ingresses(route.Namespace)

	// Create a route.
	elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
	routeClient.Create(route)
	controller.updateRouteEvent(KeyOrDie(route))

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
					Name:      ctrl.GetDomainConfigMapName(),
					Namespace: pkg.GetServingSystemNamespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
					"mytestdomain.com":  "selector:\n  app: prod",
				},
			}
			controller.SyncConfigMap(&domainConfig)
		},
	}, {
		expectedDomainSuffix: "newdefault.net",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ctrl.GetDomainConfigMapName(),
					Namespace: pkg.GetServingSystemNamespace(),
				},
				Data: map[string]string{
					"newdefault.net":   "",
					"mytestdomain.com": "selector:\n  app: prod",
				},
			}
			controller.SyncConfigMap(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}, {
		// An unrelated config map
		expectedDomainSuffix: "newdefault.net",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "SomethingDifferent",
					Namespace: pkg.GetServingSystemNamespace(),
				},
				Data: map[string]string{
					defaultDomainSuffix: "",
				},
			}
			controller.SyncConfigMap(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}, {
		// An invalid config map
		expectedDomainSuffix: "newdefault.net",
		apply: func() {
			domainConfig := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ctrl.GetDomainConfigMapName(),
					Namespace: pkg.GetServingSystemNamespace(),
				},
				Data: map[string]string{
					"mytestdomain.com": "selector:\n  app: prod",
				},
			}
			controller.SyncConfigMap(&domainConfig)
			route.Labels = make(map[string]string)
		},
	}}
	for _, expectation := range expectations {
		expectation.apply()
		elaInformer.Serving().V1alpha1().Routes().Informer().GetIndexer().Add(route)
		routeClient.Update(route)
		controller.updateRouteEvent(KeyOrDie(route))

		route, _ = routeClient.Get(route.Name, metav1.GetOptions{})
		expectedDomain := fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, expectation.expectedDomainSuffix)
		if route.Status.Domain != expectedDomain {
			t.Errorf("Expected domain %q but saw %q", expectedDomain, route.Status.Domain)
		}
		ingress, _ := ingressClient.Get(ctrl.GetElaK8SIngressName(route), metav1.GetOptions{})
		expectedHost := route.Status.Domain
		expectedWildcardHost := fmt.Sprintf("*.%s", route.Status.Domain)
		if ingress.Spec.Rules[0].Host != expectedHost {
			t.Errorf("Expected ingress host %q but saw %q", expectedHost, ingress.Spec.Rules[0].Host)
		}
		if ingress.Spec.Rules[1].Host != expectedWildcardHost {
			t.Errorf("Expected ingress host %q but saw %q", expectedWildcardHost, ingress.Spec.Rules[1].Host)
		}
	}
}
