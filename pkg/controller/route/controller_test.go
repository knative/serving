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
- When a ES is created:
  - a namespace is created

- When a Revision is updated TODO
- When a Revision is deleted TODO
*/
import (
	"fmt"
	"testing"
	"time"

	v1beta1 "k8s.io/api/extensions/v1beta1"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/google/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/google/elafros/pkg/client/informers/externalversions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

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
	}
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

func TestCreateHSCreatesStuff(t *testing.T) {
	kubeClient, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	es := getTestRoute()
	h := hooks.NewHooks()

	// Look for the placeholder service to be created.
	expectedServiceName := fmt.Sprintf("%s-service", es.Name)
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) hooks.HookResult {
		s := obj.(*corev1.Service)
		glog.Infof("checking service %s", s.Name)
		if e, a := expectedServiceName, s.Name; e != a {
			t.Errorf("unexpected service: %q expected: %q", a, e)
		}
		//TODO(grantr): The expected namespace is currently the same as the
		//Route namespace, but that may change to expectedNamespace.
		if es.Namespace != s.Namespace {
			t.Errorf("service namespace was %s, not %s", s.Namespace, es.Namespace)
		}
		//TODO nginx port
		return hooks.HookComplete
	})

	// Look for the ingress.
	expectedIngressName := fmt.Sprintf("%s-ela-ingress", es.Name)
	h.OnCreate(&kubeClient.Fake, "ingresses", func(obj runtime.Object) hooks.HookResult {
		i := obj.(*v1beta1.Ingress)
		if expectedIngressName != i.Name {
			t.Errorf("ingress was not named %s", expectedIngressName)
		}
		//TODO(grantr): The expected namespace is currently the same as the
		//Route namespace, but that may change to expectedNamespace.
		if es.Namespace != i.Namespace {
			t.Errorf("ingress namespace was %s, not %s", i.Namespace, es.Namespace)
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
	//TODO(grantr): routing is WIP so no tests yet

	elaClient.ElafrosV1alpha1().Routes("test").Create(es)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
