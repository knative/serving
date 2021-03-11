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

import (
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	network "knative.dev/networking/pkg"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	fakerouteinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestNewRouteCallsSyncHandler(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	// A standalone revision
	rev := Revision(testNamespace, "test-rev", MarkRevisionReady, WithK8sServiceName)
	// A route targeting the revision
	route := Route(testNamespace, "test-route", WithSpecTraffic(
		v1.TrafficTarget{
			RevisionName: "test-rev",
			Percent:      ptr.Int64(100),
		}))

	// Create fake clients
	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			defaultDomainSuffix: "",
			prodDomainSuffix:    "selector:\n  app: prod",
		},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
	})

	servingClient := fakeservingclient.Get(ctx)
	networkingClient := fakenetworkingclient.Get(ctx)

	h := NewHooks()
	// Check for Ingress created as a signal that syncHandler ran
	h.OnCreate(&networkingClient.Fake, "ingresses", func(obj runtime.Object) HookResult {
		ci := obj.(*netv1alpha1.Ingress)
		t.Logf("ingress created: %q", ci.Name)

		return HookComplete
	})

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	// Run the controller.
	eg := errgroup.Group{}
	eg.Go(func() error {
		ctrl := NewController(ctx, configMapWatcher)
		return ctrl.Run(2, ctx.Done())
	})

	defer func() {
		cancel()
		if err := eg.Wait(); err != nil {
			t.Fatal("Error running controller:", err)
		}
		waitInformers()
	}()

	if _, err := servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{}); err != nil {
		t.Fatal("Unexpected error creating revision:", err)
	}
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	if _, err := servingClient.ServingV1().Routes(route.Namespace).Create(ctx, route, metav1.CreateOptions{}); err != nil {
		t.Fatal("Unexpected error creating route:", err)
	}
	fakerouteinformer.Get(ctx).Informer().GetIndexer().Add(route)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Fatal(err)
	}
}
