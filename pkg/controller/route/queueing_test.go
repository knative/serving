/*
Copyright 2018 Google LLC.

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
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	ctrl "github.com/knative/serving/pkg/controller"
	. "github.com/knative/serving/pkg/controller/testing"
	"github.com/knative/serving/pkg/logging"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

const (
	testNamespace       string = "test"
	defaultDomainSuffix string = "test-domain.dev"
	prodDomainSuffix    string = "prod-domain.com"
)

var (
	testLogger = zap.NewNop().Sugar()
	testCtx    = logging.WithLogger(context.Background(), testLogger)
)

func getTestRevision(name string) *v1alpha1.Revision {
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
			Conditions: []v1alpha1.RevisionCondition{{
				Type:   v1alpha1.RevisionConditionReady,
				Status: corev1.ConditionTrue,
				Reason: "ServiceReady",
			}},
		},
	}
}

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

type receiver struct {
	kubeClient *fakekubeclientset.Clientset
}

func (r *receiver) SyncRoute(route *v1alpha1.Route) error {
	_, err := r.kubeClient.Core().Services(route.Namespace).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: route.Namespace,
			Name:      route.Name,
		},
	})
	return err
}

var _ Receiver = (*receiver)(nil)

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	c, err := NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
		ctrl.Config{
			Domains: map[string]*ctrl.LabelSelector{
				defaultDomainSuffix: &ctrl.LabelSelector{},
				prodDomainSuffix: &ctrl.LabelSelector{
					Selector: map[string]string{
						"app": "prod",
					},
				},
			},
		},
		testLogger,
		&receiver{kubeClient},
	)
	if err != nil {
		t.Fatalf("Error creating controller: %v", err)
	}
	controller = c.(*Controller)

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

func TestNewRouteCallsSyncHandler(t *testing.T) {
	// A standalone revision
	rev := getTestRevision("test-rev")
	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      100,
			},
		},
	)
	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	kubeClient, _, _, _, _, stopCh := newRunningTestController(t, rev, route)
	defer close(stopCh)

	h := NewHooks()

	// Check for service created as a signal that syncHandler ran
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) HookResult {
		service := obj.(*corev1.Service)
		t.Logf("service created: %q", service.Name)

		return HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
