/*
Copyright 2018 The Knative Authors.

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
	"testing"
	"time"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/configmap"
	ctrl "github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/route/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/controller/testing"
	. "github.com/knative/serving/pkg/logging/testing"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

func TestNewRouteCallsSyncHandler(t *testing.T) {
	// A standalone revision
	rev := getTestRevision("test-rev")
	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			RevisionName: "test-rev",
			Percent:      100,
		}},
	)

	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.

	// Create fake clients
	kubeClient := fakekubeclientset.NewSimpleClientset()
	configMapWatcher := configmap.NewFixedWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DomainConfigName,
			Namespace: pkg.GetServingSystemNamespace(),
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			prodDomainSuffix:    "selector:\n  app: prod",
		},
	})
	servingClient := fakeclientset.NewSimpleClientset(rev, route)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	controller := NewController(
		ctrl.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		servingInformer.Serving().V1alpha1().Routes(),
		servingInformer.Serving().V1alpha1().Configurations(),
	)

	h := NewHooks()

	// Check for service created as a signal that syncHandler ran
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) HookResult {
		service := obj.(*corev1.Service)
		t.Logf("service created: %q", service.Name)

		return HookComplete
	})

	stopCh := make(chan struct{})
	defer close(stopCh)
	kubeInformer.Start(stopCh)
	servingInformer.Start(stopCh)
	configMapWatcher.Start(stopCh)

	// Run the controller.
	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
