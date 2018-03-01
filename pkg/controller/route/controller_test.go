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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/golang/glog"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/google/elafros/pkg/apis/istio/v1alpha2"
	fakeclientset "github.com/google/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/google/elafros/pkg/client/informers/externalversions"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"

	elaconfig "github.com/google/elafros/pkg/controller/configuration"
	hooks "github.com/google/elafros/pkg/controller/testing"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func getTestRoute() *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/routes/test-route",
			Name:      "test-route",
			Namespace: "test",
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{
				v1alpha1.TrafficTarget{
					Revision: "test-rev",
					Percent:  100,
				},
			},
		},
	}
}

func getTestRouteWithMultipleTargets() *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/Routes/test-route",
			Name:      "test-route",
			Namespace: "test",
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{
				v1alpha1.TrafficTarget{
					Configuration: "test-config",
					Percent:       90,
				},
				v1alpha1.TrafficTarget{
					Revision: "test-rev",
					Percent:  10,
				},
			},
		},
	}
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

func getTestRevision(name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/test/revisions/%s", name),
			Name:      name,
			Namespace: "test",
			Labels:    map[string]string{},
		},
		Spec: v1alpha1.RevisionSpec{
			ContainerSpec: &corev1.Container{
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
			Template: v1alpha1.Revision{
				Spec: v1alpha1.RevisionSpec{
					ContainerSpec: &corev1.Container{
						Image: "test-image",
					},
				},
			},
		},
	}
}

func getTestRevisionForConfig(config *v1alpha1.Configuration) *v1alpha1.Revision {
	rev := config.Spec.Template.DeepCopy()
	rev.ObjectMeta = metav1.ObjectMeta{
		SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/p-deadbeef",
		Name:      "p-deadbeef",
		Namespace: "test",
		Labels: map[string]string{
			elaconfig.ConfigurationLabelKey: config.Name,
		},
	}
	rev.Status = v1alpha1.RevisionStatus{
		ServiceName: "p-deadbeef-service",
	}
	return rev
}

func newRunningTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	// Create a controller and safe cast it to the proper type. This is necessary
	// because NewController returns controller.Interface.
	controller, ok := NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
	).(*Controller)
	if !ok {
		t.Fatal("cast to *Controller failed")
	}

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
	h.OnCreate(&kubeClient.Fake, "ingresses", func(obj runtime.Object) hooks.HookResult {
		i := obj.(*v1beta1.Ingress)
		if e, a := expectedIngressName, i.Name; e != a {
			t.Errorf("unexpected ingress name: %q expected: %q", a, e)
		}
		if e, a := route.Namespace, i.Namespace; e != a {
			t.Errorf("unexpected ingress namespace: %q expected: %q", a, e)
		}
		return hooks.HookComplete
	})

	// Look for the event
	expectedMessage := MessageResourceSynced
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) hooks.HookResult {
		event := obj.(*corev1.Event)
		if e, a := expectedMessage, event.Message; e != a {
			t.Errorf("unexpected Message: %q expected: %q", a, e)
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
	// Make two revisions point to same configuration
	rev.Labels[elaconfig.ConfigurationLabelKey] = config.Name
	h := hooks.NewHooks()

	// Create a Revision when the Configuration is created to simulate the action
	// of the Configuration controller, which isn't running during this test.
	elaClient.Fake.PrependReactor("create", "configurations", func(a kubetesting.Action) (bool, runtime.Object, error) {
		cfg := a.(kubetesting.CreateActionImpl).Object.(*v1alpha1.Configuration)
		cfgrev := getTestRevisionForConfig(cfg)
		// This must be a goroutine to avoid deadlocking the Fake fixture
		go elaClient.ElafrosV1alpha1().Revisions(cfg.Namespace).Create(cfgrev)
		// Set LatestReady to this revision
		cfg.Status.LatestReady = cfgrev.Name
		// Return the modified Configuration so the object passed to later reactors
		// (including the fixture reactor) has our Status mutation
		return false, cfg, nil
	})

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

func TestSetLabelToConfigurationIndirectlyConfigured(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	rev := getTestRevisionForConfig(config)
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Revision: rev.Name,
				Percent:  100,
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
		expectedLabels := map[string]string{RouteLabelKey: route.Name}
		if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

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
				Configuration: config.Name,
				Percent:       100,
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
		expectedLabels := map[string]string{RouteLabelKey: route.Name}
		if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDeleteLabelOfConfigurationWhenUnconfigured(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	route := getTestRouteWithTrafficTargets([]v1alpha1.TrafficTarget{})
	config := getTestConfiguration()
	// Set a label which is expected to be deleted.
	config.Labels = map[string]string{RouteLabelKey: route.Name}
	rev := getTestRevisionForConfig(config)
	h := hooks.NewHooks()

	elaClient.ElafrosV1alpha1().Configurations("test").Create(config)
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	elaClient.ElafrosV1alpha1().Routes("test").Create(route)

	// Look for the configuration.
	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		// Check labels, should be empty.
		expectedLabels := map[string]string{}
		if diff := cmp.Diff(expectedLabels, config.Labels); diff != "" {
			t.Errorf("Unexpected label diff (-want +got): %v", diff)
		}
		return hooks.HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
