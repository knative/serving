/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package serverlessservice

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"

	networkingv1alpha1 "knative.dev/networking/pkg/client/clientset/versioned/typed/networking/v1alpha1"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	fakedynamicclient "knative.dev/pkg/injection/clients/dynamicclient/fake"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	_ "knative.dev/pkg/metrics/testing"
	. "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
)

func TestGlobalResyncOnActivatorChange(t *testing.T) {
	const (
		ns1  = "test-ns1"
		ns2  = "test-ns2"
		sks1 = "test-sks-1"
		sks2 = "test-sks-2"
	)
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	// Replace the fake dynamic client with one containing our objects.
	ctx, _ = fakedynamicclient.With(ctx, runtime.NewScheme(),
		ToUnstructured(t, NewScheme(), []runtime.Object{deploy(ns1, sks1), deploy(ns2, sks2)})...,
	)
	ctx = podscalable.WithDuck(ctx)
	ctrl := NewController(ctx, configmap.NewStaticWatcher())

	grp := errgroup.Group{}

	kubeClnt := fakekubeclient.Get(ctx)

	// Create activator endpoints.
	aEps := activatorEndpoints(WithSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(aEps.Namespace).Create(ctx, aEps, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating activator endpoints:", err)
	}

	// Private endpoints are supposed to exist, since we're using selector mode for the service.
	privEps := endpointspriv(ns1, sks1)
	if _, err := kubeClnt.CoreV1().Endpoints(privEps.Namespace).Create(ctx, privEps, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating private endpoints:", err)
	}
	// This is passive, so no endpoints.
	privEps = endpointspriv(ns2, sks2, withOtherSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(privEps.Namespace).Create(ctx, privEps, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating private endpoints:", err)
	}

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Error starting informers:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Fatal("Error waiting for contoller to terminate:", err)
		}
		waitInformers()
	}()

	grp.Go(func() error {
		return ctrl.Run(1, ctx.Done())
	})

	networking := fakenetworkingclient.Get(ctx).NetworkingV1alpha1()

	// Inactive, will reconcile.
	sksObj1 := SKS(ns1, sks1, WithPrivateService, WithPubService, WithDeployRef(sks1), WithProxyMode)
	sksObj1.Generation = 1
	// Active, should not visibly reconcile.
	sksObj2 := SKS(ns2, sks2, WithPrivateService, WithPubService, WithDeployRef(sks2), markHappy)
	sksObj2.Generation = 1

	if _, err := networking.ServerlessServices(ns1).Create(ctx, sksObj1, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating SKS1:", err)
	}
	if _, err := networking.ServerlessServices(ns2).Create(ctx, sksObj2, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating SKS2:", err)
	}

	// Wait for the SKSs to be reconciled
	waitForObservedGen(ctx, networking, ns1, sks1, 1)
	waitForObservedGen(ctx, networking, ns2, sks2, 1)

	t.Log("Updating the activator endpoints now...")
	// Now that we have established the baseline, update the activator endpoints.
	aEps = activatorEndpoints(withOtherSubsets)
	if _, err := kubeClnt.CoreV1().Endpoints(aEps.Namespace).Update(ctx, aEps, metav1.UpdateOptions{}); err != nil {
		t.Fatal("Error creating activator endpoints:", err)
	}

	// Actively wait for the endpoints to change their value.
	eps := fakeendpointsinformer.Get(ctx).Lister()
	if err := wait.PollImmediate(10*time.Millisecond, 3*time.Second, func() (bool, error) {
		ep, err := eps.Endpoints(ns1).Get(sks1)
		if err != nil {
			return false, err
		}
		if cmp.Equal(ep.Subsets, resources.FilterSubsetPorts(sksObj1, aEps.Subsets)) {
			return true, nil
		}
		return false, nil
	}); err != nil {
		t.Fatal("Failed to see Public Endpoints propagation:", err)
	}
}

func waitForObservedGen(ctx context.Context, client networkingv1alpha1.NetworkingV1alpha1Interface, ns, name string, generation int64) error {
	return wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		sks, err := client.ServerlessServices(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return sks.Status.ObservedGeneration == 1, nil
	})
}
