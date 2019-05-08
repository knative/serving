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

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/system"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/reconciler/route/config"
	"github.com/knative/serving/pkg/reconciler/route/resources"
	"github.com/knative/serving/pkg/reconciler/route/traffic"

	. "github.com/knative/pkg/logging/testing"
)

func TestReconcileClusterIngress_Insert(t *testing.T) {
	_, servingClient, c, _, _, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	ci := newTestClusterIngress(t, r)
	if _, err := c.reconcileClusterIngress(TestContextWithLogger(t), r, ci); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	created := getRouteIngressFromClient(t, servingClient, r)
	if diff := cmp.Diff(ci, created); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
	}
}

func TestReconcileClusterIngress_Update(t *testing.T) {
	_, servingClient, c, _, servingInformer, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}

	ci := newTestClusterIngress(t, r)
	if _, err := c.reconcileClusterIngress(TestContextWithLogger(t), r, ci); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated := getRouteIngressFromClient(t, servingClient, r)
	servingInformer.Networking().V1alpha1().ClusterIngresses().Informer().GetIndexer().Add(updated)

	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   "bar.com",
	}
	ci2 := newTestClusterIngress(t, r)
	if _, err := c.reconcileClusterIngress(TestContextWithLogger(t), r, ci2); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated = getRouteIngressFromClient(t, servingClient, r)
	if diff := cmp.Diff(ci2, updated); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
	}
	if diff := cmp.Diff(ci, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileTargetRevisions(t *testing.T) {
	_, _, c, _, _, _ := newTestReconciler(t)
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
				TrafficTarget: v1beta1.TrafficTarget{
					RevisionName: "revision",
					Percent:      100,
				},
				Active: true,
			}}}},
	}, {
		name: "invalid target revision",
		tc: traffic.Config{Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1beta1.TrafficTarget{
					RevisionName: "inal-revision",
					Percent:      100,
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
			err := c.reconcileTargetRevisions(ctx, &tc.tc, r)
			if err != tc.expectErr {
				t.Fatalf("Expected err %v got %v", tc.expectErr, err)
			}
		})

		// TODO(greghaynes): Assert annotations correctly added
	}
}

func newTestClusterIngress(t *testing.T, r *v1alpha1.Route) *netv1alpha1.ClusterIngress {
	tc := &traffic.Config{Targets: map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1beta1.TrafficTarget{
				RevisionName: "revision",
				Percent:      100,
			},
			Active: true,
		}}}}
	tls := []netv1alpha1.ClusterIngressTLS{
		{
			Hosts:             []string{"test-route.test-ns.example.com"},
			PrivateKey:        "tls.key",
			SecretName:        "test-secret",
			SecretNamespace:   "test-ns",
			ServerCertificate: "tls.crt",
		},
	}
	ingress, err := resources.MakeClusterIngress(getContext(), r, tc, tls, "foo-ingress")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	return ingress
}

func TestReconcileCertificates_Insert(t *testing.T) {
	_, servingClient, c, _, _, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	certificate := newCerts([]string{"*.default.example.com"}, r)
	if _, err := c.reconcileCertificate(TestContextWithLogger(t), r, certificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	created := getCertificateFromClient(t, servingClient, certificate)
	if diff := cmp.Diff(certificate, created); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
	}
}

func TestReconcileCertificate_Update(t *testing.T) {
	_, servingClient, c, _, servingInformer, _ := newTestReconciler(t)
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	certificate := newCerts([]string{"old.example.com"}, r)
	if _, err := c.reconcileCertificate(TestContextWithLogger(t), r, certificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	storedCert := getCertificateFromClient(t, servingClient, certificate)
	servingInformer.Networking().V1alpha1().Certificates().Informer().GetIndexer().Add(storedCert)

	newCertificate := newCerts([]string{"new.example.com"}, r)
	if _, err := c.reconcileCertificate(TestContextWithLogger(t), r, newCertificate); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	updated := getCertificateFromClient(t, servingClient, newCertificate)
	if diff := cmp.Diff(newCertificate, updated); diff != "" {
		t.Errorf("Unexpected diff (-want +got): %v", diff)
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
	ctx := context.Background()
	cfg := ReconcilerTestConfig(false)
	return config.ToContext(ctx, cfg)
}
