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
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakeingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/serving/pkg/testing/v1"
)

func TestReconcileIngressInsert(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")
	tc, tls := testIngressParams(t, r)
	if _, err := reconciler.reconcileIngress(updateContext(ctx), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}
}

func TestReconcileIngressUpdate(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")

	tc, tls := testIngressParams(t, r)
	if _, err := reconciler.reconcileIngress(updateContext(ctx), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	initial := getRouteIngressFromClient(ctx, t, r)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(initial)

	tc, tls = testIngressParams(t, r, func(tc *traffic.Config) {
		tc.Targets[traffic.DefaultTarget][0].TrafficTarget.Percent = ptr.Int64(50)
		tc.Targets[traffic.DefaultTarget] = append(tc.Targets[traffic.DefaultTarget], traffic.RevisionTarget{
			TrafficTarget: v1.TrafficTarget{
				Percent:      ptr.Int64(50),
				RevisionName: "revision2",
			},
		})
	})
	if _, err := reconciler.reconcileIngress(updateContext(ctx), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	if diff := cmp.Diff(initial, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileIngressBadRollout(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()
	ctx = updateContext(ctx)

	r := Route("test-ns", "test-route")

	tc, tls := testIngressParams(t, r)
	ing, err := resources.MakeIngressWithRollout(ctx, r, tc, &traffic.Rollout{
		Configurations: []traffic.ConfigurationRollout{{
			ConfigurationName: "configuration",
			Percent:           202, // <- this is not right!
		}},
	}, tls, "foo-ingress")
	if err != nil {
		t.Fatal("Error creating ingress:", err)
	}
	ingClient := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Ingresses(ing.Namespace)
	ingClient.Create(ctx, ing, metav1.CreateOptions{})
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ing)
	if _, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}
	ing, err = ingClient.Get(ctx, ing.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Could not get the ingress:", err)
	}
	// We want to ignore the previous one, since it's bogus.
	want := func() string {
		d, _ := json.Marshal(tc.BuildRollout())
		return string(d)
	}()
	if got := ing.Annotations[networking.RolloutAnnotationKey]; got != want {
		t.Errorf("Incorrect Rollout Annotation; diff(-want,+got):\n%s", cmp.Diff(want, got))
	}
}

func TestReconcileIngressUnparseableRollout(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()
	ctx = updateContext(ctx)

	r := Route("test-ns", "test-route")

	tc, tls := testIngressParams(t, r)
	ing, err := resources.MakeIngress(ctx, r, tc, tls, "foo-ingress")
	if err != nil {
		t.Fatal("Error creating ingress:", err)
	}
	ing.Annotations[networking.RolloutAnnotationKey] = `<?xml version="1.0" encoding="utf-8"?><rollout name="from-the-wrong-universe"/>`
	ingClient := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Ingresses(ing.Namespace)
	ingClient.Create(ctx, ing, metav1.CreateOptions{})
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ing)
	if _, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}
	ing, err = ingClient.Get(ctx, ing.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Could not get the ingress:", err)
	}
	// We want to ignore the previous one, since it's bogus.
	want := func() string {
		d, _ := json.Marshal(tc.BuildRollout())
		return string(d)
	}()
	if got := ing.Annotations[networking.RolloutAnnotationKey]; got != want {
		t.Errorf("Incorrect Rollout Annotation; diff(-want,+got):\n%s", cmp.Diff(want, got))
	}
}

func TestReconcileRevisionTargetDoesNotExist(t *testing.T) {
	ctx, _, _, _, cancel := newTestSetup(t)
	defer cancel()

	r := Route("test-ns", "test-route", WithRouteLabel(map[string]string{"route": "test-route"}))
	rev := newTestRevision(r.Namespace, "revision")
	fakeservingclient.Get(ctx).ServingV1().Revisions(r.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)
}

func newTestRevision(namespace, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.RevisionSpec{},
	}
}

func testIngressParams(t *testing.T, r *v1.Route, trafficOpts ...func(tc *traffic.Config)) (*traffic.Config,
	[]netv1alpha1.IngressTLS) {
	tc := &traffic.Config{Targets: map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "revision",
				Percent:           ptr.Int64(100),
				LatestRevision:    ptr.Bool(true),
			},
		}}}}

	for _, opt := range trafficOpts {
		opt(tc)
	}

	tls := []netv1alpha1.IngressTLS{{
		Hosts:           []string{"test-route.test-ns.example.com"},
		SecretName:      "test-secret",
		SecretNamespace: "test-ns",
	}}
	return tc, tls
}

func TestReconcileIngressClassAnnotation(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	t.Cleanup(cancel)

	const expClass = "foo.ingress.networking.knative.dev"

	r := Route("test-ns", "test-route")
	tc, tls := testIngressParams(t, r)
	if _, err := reconciler.reconcileIngress(updateContext(ctx), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(updated)

	if _, err := reconciler.reconcileIngress(updateContext(ctx), r, tc, tls, expClass); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated = getRouteIngressFromClient(ctx, t, r)
	updatedClass := updated.Annotations[networking.IngressClassAnnotationKey]
	if expClass != updatedClass {
		t.Errorf("Unexpected annotation got %q want %q", expClass, updatedClass)
	}
}

func updateContext(ctx context.Context) context.Context {
	cfg := reconcilerTestConfig(false)
	return config.ToContext(ctx, cfg)
}

func getContext() context.Context {
	return updateContext(context.Background())
}
