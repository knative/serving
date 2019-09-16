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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakecertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	fakeciinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress/fake"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/pkg/logging/testing"
)

func TestReconcileClusterIngressInsert(t *testing.T) {
	ctx, _, reconciler, _, cancel := newTestReconciler(t)
	defer cancel()

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	ci := newTestClusterIngress(t, r)

	ira := &ClusterIngressResources{
		BaseIngressResources: BaseIngressResources{
			servingClientSet: reconciler.ServingClientSet,
		},
		clusterIngressLister: reconciler.clusterIngressLister,
	}

	if _, err := reconciler.reconcileIngress(TestContextWithLogger(t), ira, r, ci, true /* optional*/); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	created := getRouteClusterIngressFromClient(ctx, t, r)
	if created != nil {
		t.Errorf("Unexpected ClusterIngress creation")
	}
}

func TestReconcileClusterIngressUpdate(t *testing.T) {
	ctx, _, reconciler, _, cancel := newTestReconciler(t)
	defer cancel()

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}

	ira := &ClusterIngressResources{
		BaseIngressResources: BaseIngressResources{
			servingClientSet: reconciler.ServingClientSet,
		},
		clusterIngressLister: reconciler.clusterIngressLister,
	}

	ci := newTestClusterIngress(t, r)
	if _, err := reconciler.reconcileIngress(TestContextWithLogger(t), ira, r, ci, false /*optional*/); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated := getRouteClusterIngressFromClient(ctx, t, r)
	fakeciinformer.Get(ctx).Informer().GetIndexer().Add(updated)

	ci2 := newTestClusterIngress(t, r, func(tc *traffic.Config) {
		tc.Targets[traffic.DefaultTarget][0].TrafficTarget.Percent = ptr.Int64(50)
		tc.Targets[traffic.DefaultTarget] = append(tc.Targets[traffic.DefaultTarget], traffic.RevisionTarget{
			TrafficTarget: v1.TrafficTarget{
				Percent:      ptr.Int64(50),
				RevisionName: "revision2",
			},
		})
	})
	if _, err := reconciler.reconcileIngress(TestContextWithLogger(t), ira, r, ci2, true /*optional*/); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated = getRouteClusterIngressFromClient(ctx, t, r)
	if diff := cmp.Diff(ci2, updated); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
	}
	if diff := cmp.Diff(ci, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileTargetRevisions(t *testing.T) {
	_, _, reconciler, _, cancel := newTestReconciler(t)
	defer cancel()

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
	}

	cases := []struct {
		name      string
		tc        traffic.Config
		expectErr error
	}{{
		name: "Valid target revision",
		tc: traffic.Config{Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "revision",
					Percent:      ptr.Int64(100),
				},
				Active: true,
			}}}},
	}, {
		name: "invalid target revision",
		tc: traffic.Config{Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					RevisionName: "inal-revision",
					Percent:      ptr.Int64(100),
				},
				Active: true,
			}}}},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				GC: &gc.Config{
					StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
				},
			})
			err := reconciler.reconcileTargetRevisions(ctx, &tc.tc, r)
			if err != tc.expectErr {
				t.Fatalf("Got error = %v, want: %v", err, tc.expectErr)
			}
		})

		// TODO(greghaynes): Assert annotations correctly added
	}
}

func newTestClusterIngress(t *testing.T, r *v1alpha1.Route, trafficOpts ...func(tc *traffic.Config)) netv1alpha1.IngressAccessor {
	tc := &traffic.Config{Targets: map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "revision",
				Percent:      ptr.Int64(100),
			},
			Active: true,
		}}}}

	for _, opt := range trafficOpts {
		opt(tc)
	}

	tls := []netv1alpha1.IngressTLS{
		{
			Hosts:             []string{"test-route.test-ns.example.com"},
			PrivateKey:        "tls.key",
			SecretName:        "test-secret",
			SecretNamespace:   "test-ns",
			ServerCertificate: "tls.crt",
		},
	}
	ingress, err := resources.MakeClusterIngress(getContext(), r, tc, tls, sets.NewString(), "foo-ingress")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	return ingress
}

func TestReconcileCertificatesInsert(t *testing.T) {
	ctx, _, reconciler, _, cancel := newTestReconciler(t)
	defer cancel()

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	certificate := newCerts([]string{"*.default.example.com"}, r)
	if _, err := reconciler.reconcileCertificate(TestContextWithLogger(t), r, certificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	created := getCertificateFromClient(t, ctx, certificate)
	if diff := cmp.Diff(certificate, created); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %s", diff)
	}
}

func TestReconcileCertificateUpdate(t *testing.T) {
	ctx, _, reconciler, _, cancel := newTestReconciler(t)
	defer cancel()

	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	certificate := newCerts([]string{"old.example.com"}, r)
	if _, err := reconciler.reconcileCertificate(TestContextWithLogger(t), r, certificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	storedCert := getCertificateFromClient(t, ctx, certificate)
	fakecertinformer.Get(ctx).Informer().GetIndexer().Add(storedCert)

	newCertificate := newCerts([]string{"new.example.com"}, r)
	if _, err := reconciler.reconcileCertificate(TestContextWithLogger(t), r, newCertificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated := getCertificateFromClient(t, ctx, newCertificate)
	if diff := cmp.Diff(newCertificate, updated); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %s", diff)
	}
	if diff := cmp.Diff(certificate, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func newCerts(dnsNames []string, r *v1alpha1.Route) *netv1alpha1.Certificate {
	return &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cert",
			Namespace:       system.Namespace(),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: "test-secret",
		},
	}
}

func getContext() context.Context {
	cfg := ReconcilerTestConfig(false)
	return config.ToContext(context.Background(), cfg)
}
