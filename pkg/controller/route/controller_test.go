/*
Copyright 2017 The Kubernetes Authors.

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
	"github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	hooks "github.com/elafros/elafros/pkg/controller/testing"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func getTestRoute() *v1alpha1.Route {
	return getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      100,
			},
		},
	)
}

func getTestRouteWithTrafficTargets(traffic []v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/Routes/test-route",
			Name:      "test-route",
			Namespace: "test",
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
}

func getTestRouteWithMultipleTargets() *v1alpha1.Route {
	return getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				ConfigurationName: "test-config",
				Percent:           90,
			},
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      10,
			},
		},
	)
}

func getTestRouteWithDuplicateTargets() *v1alpha1.Route {
	return getTestRouteWithTrafficTargets(
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
}

func getTestRevision(name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/test/revisions/%s", name),
			Name:      name,
			Namespace: "test",
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
			Namespace: "test",
		},
		Spec: v1alpha1.ConfigurationSpec{
			// This is a workaround for generation initialization
			Generation: 1,
			RevisionTemplate: v1alpha1.Revision{
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
	rev := config.Spec.RevisionTemplate.DeepCopy()
	rev.ObjectMeta = metav1.ObjectMeta{
		SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/p-deadbeef",
		Name:      "p-deadbeef",
		Namespace: "test",
		Labels: map[string]string{
			ela.ConfigurationLabelKey: config.Name,
		},
	}
	rev.Status = v1alpha1.RevisionStatus{
		ServiceName: "p-deadbeef-service",
	}
	return rev
}

func newTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset()

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

func newRunningTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, elaClient, controller, kubeInformer, elaInformer = newTestController(t)

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

func keyOrDie(obj interface{}) string {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		panic(err)
	}
	return key
}

func TestCreateRouteCreatesStuff(t *testing.T) {
	kubeClient, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	route := getTestRoute()
	rev := getTestRevision("test-rev")
	h := hooks.NewHooks()

	// Look for the placeholder service to be created.
	expectedServiceName := fmt.Sprintf("%s-service", route.Name)
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) hooks.HookResult {
		s := obj.(*corev1.Service)
		glog.Infof("checking service %s", s.Name)
		if e, a := expectedServiceName, s.Name; e != a {
			t.Errorf("unexpected service: %q expected: %q", a, e)
		}
		if e, a := route.Namespace, s.Namespace; e != a {
			t.Errorf("unexpected route namespace: %q expected: %q", a, e)
		}

		expectedPort := corev1.ServicePort{
			Name: "http",
			Port: 80,
		}

		if len(s.Spec.Ports) != 1 || s.Spec.Ports[0] != expectedPort {
			t.Error("expected a single service port http/80")
		}
		return hooks.HookComplete
	})

	// Look for the ingress.
	expectedIngressName := fmt.Sprintf("%s-ela-ingress", route.Name)
	expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
	h.OnCreate(&kubeClient.Fake, "ingresses", func(obj runtime.Object) hooks.HookResult {
		i := obj.(*v1beta1.Ingress)
		if e, a := expectedIngressName, i.Name; e != a {
			t.Errorf("unexpected ingress name: %q expected: %q", a, e)
		}
		if e, a := route.Namespace, i.Namespace; e != a {
			t.Errorf("unexpected ingress namespace: %q expected: %q", a, e)
		}
		if !strings.HasPrefix(i.Spec.Rules[0].Host, expectedDomainPrefix) {
			t.Errorf("Ingress host '%s' must have prefix '%s'", i.Spec.Rules[0].Host, expectedDomainPrefix)
		}
		return hooks.HookComplete
	})

	// Look for the event
	expectedMessages := map[string]struct{}{
		"Created service \"test-route-service\"":        struct{}{},
		"Created Ingress \"test-route-ela-ingress\"":    struct{}{},
		"Created Istio route rule \"test-route-istio\"": struct{}{},
		"Updated status for route \"test-route\"":       struct{}{},
	}
	eventNum := 0
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) hooks.HookResult {
		event := obj.(*corev1.Event)
		eventNum = eventNum + 1
		if _, ok := expectedMessages[event.Message]; !ok {
			t.Errorf("unexpected Message: %q expected one of: %q", event.Message, expectedMessages)
		}
		if wanted, got := corev1.EventTypeNormal, event.Type; wanted != got {
			t.Errorf("unexpected event Type: %q expected: %q", got, wanted)
		}
		// Expect 4 events.
		if eventNum < 4 {
			return hooks.HookIncomplete
		}
		return hooks.HookComplete
	})

	// Look for the route.
	h.OnCreate(&elaClient.Fake, "routerules", func(obj runtime.Object) hooks.HookResult {
		rule := obj.(*v1alpha2.RouteRule)

		// Check labels
		expectedLabels := map[string]string{"route": route.Name}
		if diff := cmp.Diff(expectedLabels, rule.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}

		// Check owner refs
		expectedRefs := []metav1.OwnerReference{
			metav1.OwnerReference{
				APIVersion: "elafros.dev/v1alpha1",
				Kind:       "Route",
				Name:       "test-route",
			},
		}

		if diff := cmp.Diff(expectedRefs, rule.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
			t.Errorf("Unexpected rule owner refs diff (-want +got): %v", diff)
		}

		expectedRouteSpec := v1alpha2.RouteRuleSpec{
			Destination: v1alpha2.IstioService{
				Name: "test-route-service",
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

		if diff := cmp.Diff(expectedRouteSpec, rule.Spec); diff != "" {
			t.Errorf("Unexpected rule spec diff (-want +got): %s", diff)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteWithMultipleTargets(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	route := getTestRouteWithMultipleTargets()
	rev := getTestRevision("test-rev")
	config := getTestConfiguration()
	h := hooks.NewHooks()

	// Create a Revision when the Configuration is created to simulate the action
	// of the Configuration controller, which isn't running during this test.
	elaClient.Fake.PrependReactor("create", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			cfg := a.(kubetesting.CreateActionImpl).Object.(*v1alpha1.Configuration)
			cfgrev := getTestRevisionForConfig(cfg)
			// This must be a goroutine to avoid deadlocking the Fake fixture
			go elaClient.ElafrosV1alpha1().Revisions(cfg.Namespace).Create(cfgrev)
			// Set LatestReadyRevisionName to this revision
			cfg.Status.LatestReadyRevisionName = cfgrev.Name
			// Return the modified Configuration so the object passed to later reactors
			// (including the fixture reactor) has our Status mutation
			return false, cfg, nil
		},
	)

	// Look for the route.
	h.OnCreate(&elaClient.Fake, "routerules", func(obj runtime.Object) hooks.HookResult {
		rule := obj.(*v1alpha2.RouteRule)

		expectedRouteSpec := v1alpha2.RouteRuleSpec{
			Destination: v1alpha2.IstioService{
				Name: "test-route-service",
			},
			Route: []v1alpha2.DestinationWeight{
				v1alpha2.DestinationWeight{
					Destination: v1alpha2.IstioService{
						Name: "p-deadbeef-service.test",
					},
					Weight: 90,
				},
				v1alpha2.DestinationWeight{
					Destination: v1alpha2.IstioService{
						Name: "test-rev-service.test",
					},
					Weight: 10,
				},
			},
		}

		if diff := cmp.Diff(expectedRouteSpec, rule.Spec); diff != "" {
			t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRouteWithDuplicateTargets(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	route := getTestRouteWithDuplicateTargets()
	rev := getTestRevision("test-rev")
	config := getTestConfiguration()
	h := hooks.NewHooks()

	// Create a Revision when the Configuration is created to simulate the action
	// of the Configuration controller, which isn't running during this test.
	elaClient.Fake.PrependReactor("create", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			cfg := a.(kubetesting.CreateActionImpl).Object.(*v1alpha1.Configuration)
			cfgrev := getTestRevisionForConfig(cfg)
			// This must be a goroutine to avoid deadlocking the Fake fixture
			go elaClient.ElafrosV1alpha1().Revisions(cfg.Namespace).Create(cfgrev)
			// Set LatestReadyRevisionName to this revision
			cfg.Status.LatestReadyRevisionName = cfgrev.Name
			// Return the modified Configuration so the object passed to later reactors
			// (including the fixture reactor) has our Status mutation
			return false, cfg, nil
		},
	)

	// Look for the route.
	h.OnCreate(&elaClient.Fake, "routerules", func(obj runtime.Object) hooks.HookResult {
		rule := obj.(*v1alpha2.RouteRule)

		expectedRouteSpec := v1alpha2.RouteRuleSpec{
			Destination: v1alpha2.IstioService{
				Name: "test-route-service",
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

		if diff := cmp.Diff(expectedRouteSpec, rule.Spec); diff != "" {
			t.Errorf("Unexpected rule spec diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestSetLabelToConfigurationDirectlyConfigured(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
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
	h := hooks.NewHooks()

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	// Look for the configuration.
	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		// Check labels
		expectedLabels := map[string]string{ela.RouteLabelKey: route.Name}
		if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestSetLabelToConfigurationIndirectlyConfigured(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
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
	h := hooks.NewHooks()

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	// Look for the configuration.
	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		// Check labels
		expectedLabels := map[string]string{ela.RouteLabelKey: route.Name}
		if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
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

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)
	// Since syncHandler looks in the lister, we need to add it to the informer
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
	err := controller.syncHandler(route.Namespace + "/" + route.Name)
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

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Routes().Informer().GetIndexer().Add(route)

	// No configuration updates
	elaClient.Fake.PrependReactor("update", "configurations",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Configuration was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.syncHandler(route.Namespace + "/" + route.Name)
}

func TestDeleteLabelOfConfigurationWhenUnconfigured(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	config := getTestConfiguration()
	// Set a label which is expected to be deleted.
	config.Labels = map[string]string{ela.RouteLabelKey: route.Name}
	rev := getTestRevisionForConfig(config)
	h := hooks.NewHooks()

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	// Look for the configuration.
	h.OnUpdate(&elaClient.Fake, "configurations",
		func(obj runtime.Object) hooks.HookResult {
			config := obj.(*v1alpha1.Configuration)
			// Check labels, should be empty.
			expectedLabels := map[string]string{}
			if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
				t.Errorf("Unexpected label diff (-want +got): %v", diff)
			}
			return hooks.HookComplete
		},
	)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestUpdateRouteWhenConfigurationChanges(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
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
	h := hooks.NewHooks()

	// Look for the route.
	h.OnCreate(&elaClient.Fake, "routes", func(obj runtime.Object) hooks.HookResult {
		route := obj.(*v1alpha1.Route)
		// The configuration has no LatestReadyRevisionName, nothing should be changing.
		var expectedTrafficTargets []v1alpha1.TrafficTarget
		if diff := cmp.Diff(expectedTrafficTargets, route.Status.Traffic); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}

		// Update config to has LatestReadyRevisionName and route label.
		config.Status.LatestReadyRevisionName = rev.Name
		config.Labels = map[string]string{
			ela.RouteLabelKey: route.Name,
		}
		go elaClient.ElafrosV1alpha1().Configurations("test").Update(config)
		return hooks.HookComplete
	})

	h.OnUpdate(&elaClient.Fake, "routes", func(obj runtime.Object) hooks.HookResult {
		route := obj.(*v1alpha1.Route)
		expectedTrafficTargets := []v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: rev.Name,
				Percent:      100,
			},
		}
		if diff := cmp.Diff(expectedTrafficTargets, route.Status.Traffic); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		expectedDomainPrefix := fmt.Sprintf("%s.%s.", route.Name, route.Namespace)
		if !strings.HasPrefix(route.Status.Domain, expectedDomainPrefix) {
			t.Errorf("Route domain '%s' must have prefix '%s'", route.Status.Domain, expectedDomainPrefix)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Routes("test").Create(route)
	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
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

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)
	// Since syncHandler looks in the lister, we need to add it to the informer
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
