/*
   Copyright 2019 The Knative Authors

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

package serverlessservice

import (
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	ctrl "github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/activator"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	preconciler "github.com/knative/serving/pkg/reconciler"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	. "github.com/knative/pkg/reconciler/testing"
	"github.com/knative/serving/pkg/reconciler/serverlessservice/resources/names"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

// objs are used to seed dynamic client only currently.
func newTestController(t *testing.T, stopCh <-chan struct{}, objs []runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *ctrl.Impl,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory) {
	// Create fake clients.
	kubeClient = fakekubeclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset()
	dynamicClient := fakedynamic.NewSimpleDynamicClient(
		runtime.NewScheme(), ToUnstructured(t, objs)...)

	opt := preconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		DynamicClientSet: dynamicClient,

		Logger:       logtesting.TestLogger(t),
		ResyncPeriod: 0,
		StopChannel:  stopCh,
		Recorder:     record.NewFakeRecorder(1000),
	}

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, opt.ResyncPeriod)
	servingInformer = informers.NewSharedInformerFactory(servingClient, opt.ResyncPeriod)

	controller = NewController(
		opt,
		servingInformer.Networking().V1alpha1().ServerlessServices(),
		kubeInformer.Core().V1().Services(),
		kubeInformer.Core().V1().Endpoints(),
	)

	return
}

func TestGlobalResyncOnActivatorChange(t *testing.T) {
	const (
		ns1  = "test-ns1"
		ns2  = "test-ns2"
		sks1 = "test-sks-1"
		sks2 = "test-sks-2"
	)
	defer logtesting.ClearAll()
	stopCh := make(chan struct{})
	kubeClnt, servClnt, controller, kubeInformer, servingInformer := newTestController(t, stopCh,
		[]runtime.Object{
			deploy(ns1, sks1),
			deploy(ns2, sks2),
		},
	)
	grp := errgroup.Group{}
	defer func() {
		close(stopCh)
		if err := grp.Wait(); err != nil {
			t.Fatalf("Error waiting for contoller to terminate: %v", err)
		}
	}()

	// Create activator endpoints.
	aEps := activatorEndpoints(WithSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(aEps.Namespace).Create(aEps); err != nil {
		t.Fatalf("Error creating activator endpoints: %v", err)
	}

	// Private endpoints are supposed to exist, since we're using selector mode for the service.
	privEps := endpointspriv(ns1, sks1)
	if _, err := kubeClnt.CoreV1().Endpoints(privEps.Namespace).Create(privEps); err != nil {
		t.Fatalf("Error creating private endpoints: %v", err)
	}
	// This is passive, so no endpoints.
	privEps = endpointspriv(ns2, sks2, withOtherSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(privEps.Namespace).Create(privEps); err != nil {
		t.Fatalf("Error creating private endpoints: %v", err)
	}

	if err := ctrl.StartInformers(
		stopCh,
		servingInformer.Networking().V1alpha1().ServerlessServices().Informer(),
		kubeInformer.Core().V1().Endpoints().Informer(),
		kubeInformer.Core().V1().Services().Informer()); err != nil {
		t.Fatalf("Error starting informers: %v", err)
	}

	grp.Go(func() error {
		return controller.Run(1, stopCh)
	})

	numServices, numEndpoints := 0, 0
	hooks := NewHooks()
	hooks.OnCreate(&kubeClnt.Fake, "endpoints", func(obj runtime.Object) HookResult {
		t.Logf("Registered creation of endpoints: %#v", obj)
		// We are waiting for creation of two endpoints objects.
		numEndpoints++
		if numEndpoints == 2 {
			return HookComplete
		}
		return HookIncomplete
	})
	hooks.OnCreate(&kubeClnt.Fake, "services", func(obj runtime.Object) HookResult {
		t.Logf("Registered creation of services: %#v", obj)
		numServices++
		// We need to wait for creation of 2x2 K8s services.
		if numServices == 4 {
			return HookComplete
		}
		return HookIncomplete
	})

	// Inactive, will reconcile.
	sksObj1 := SKS(ns1, sks1, WithPrivateService, WithPubService, WithDeployRef(sks1), WithProxyMode)
	// Active, should not visibly reconcile.
	sksObj2 := SKS(ns2, sks2, WithPrivateService, WithPubService, WithDeployRef(sks2), markHappy)

	if _, err := servClnt.NetworkingV1alpha1().ServerlessServices(ns1).Create(sksObj1); err != nil {
		t.Fatalf("Error creating SKS1: %v", err)
	}
	if _, err := servClnt.NetworkingV1alpha1().ServerlessServices(ns2).Create(sksObj2); err != nil {
		t.Fatalf("Error creating SKS2: %v", err)
	}
	if err := hooks.WaitForHooks(3 * time.Second); err != nil {
		t.Fatalf("Error creating preliminary objects: %v", err)
	}

	t.Log("Updating the activator endpoints now...")
	// Now that we have established the baseline, update the activator endpoints.
	// Reset the hooks.
	hooks = NewHooks()
	hooks.OnUpdate(&kubeClnt.Fake, "endpoints", func(obj runtime.Object) HookResult {
		eps := obj.(*corev1.Endpoints)
		if eps.Name == names.PublicService(sks1) {
			t.Logf("Registering expected hook update for endpoints %s", eps.Name)
			return HookComplete
		}
		if eps.Name == activator.K8sServiceName {
			// Expected, but not the one we're waiting for.
			t.Log("Registering activator endpoint update")
		} else {
			// Somethings broken.
			t.Errorf("Unexpected endpoint update for %s", eps.Name)
		}
		return HookIncomplete
	})

	aEps = activatorEndpoints(withOtherSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(aEps.Namespace).Update(aEps); err != nil {
		t.Fatalf("Error creating activator endpoints: %v", err)
	}

	if err := hooks.WaitForHooks(3 * time.Second); err != nil {
		t.Fatalf("Hooks timed out: %v", err)
	}
}
