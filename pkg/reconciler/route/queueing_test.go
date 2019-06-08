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
	"context"
	"testing"
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/system"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	fakeservingclient "github.com/knative/serving/pkg/client/injection/client/fake"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	. "github.com/knative/pkg/reconciler/testing"
)

func TestNewRouteCallsSyncHandler(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, informers := SetupFakeContext(t)

	// A standalone revision
	rev := getTestRevision("test-rev")
	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{{
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      100,
			},
		}},
	)

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
	})

	ctrl := NewController(ctx, configMapWatcher)

	servingClient := fakeservingclient.Get(ctx)

	h := NewHooks()

	// Check for ClusterIngress created as a signal that syncHandler ran
	h.OnCreate(&servingClient.Fake, "clusteringresses", func(obj runtime.Object) HookResult {
		ci := obj.(*netv1alpha1.ClusterIngress)
		t.Logf("ingress created: %q", ci.Name)

		return HookComplete
	})

	ctx, cancel := context.WithCancel(ctx)
	eg := errgroup.Group{}
	defer func() {
		cancel()
		if err := eg.Wait(); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	// Run the controller.
	eg.Go(func() error {
		return ctrl.Run(2, ctx.Done())
	})

	if _, err := servingClient.ServingV1alpha1().Revisions(rev.Namespace).Create(rev); err != nil {
		t.Errorf("Unexpected error creating revision: %v", err)
	}

	if _, err := servingClient.ServingV1alpha1().Routes(route.Namespace).Create(route); err != nil {
		t.Errorf("Unexpected error creating route: %v", err)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
