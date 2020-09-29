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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakecertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	fakeciinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/serving/pkg/testing/v1"
)

func getCertificateFromClient(ctx context.Context, t *testing.T, desired *netv1alpha1.Certificate) *netv1alpha1.Certificate {
	t.Helper()
	created, err := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Certificates(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Certificates(%s).Get(%s) = %v", desired.Namespace, desired.Name, err)
	}
	return created
}

func TestReconcileIngressInsert(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")
	ci := newTestIngress(t, r)

	if _, err := reconciler.reconcileIngress(ctx, r, ci); err != nil {
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

	ci := newTestIngress(t, r)
	if _, err := reconciler.reconcileIngress(ctx, r, ci); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	fakeciinformer.Get(ctx).Informer().GetIndexer().Add(updated)

	ci2 := newTestIngress(t, r, func(tc *traffic.Config) {
		tc.Targets[traffic.DefaultTarget][0].TrafficTarget.Percent = ptr.Int64(50)
		tc.Targets[traffic.DefaultTarget] = append(tc.Targets[traffic.DefaultTarget], traffic.RevisionTarget{
			TrafficTarget: v1.TrafficTarget{
				Percent:      ptr.Int64(50),
				RevisionName: "revision2",
			},
		})
	})
	if _, err := reconciler.reconcileIngress(ctx, r, ci2); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated = getRouteIngressFromClient(ctx, t, r)
	if diff := cmp.Diff(ci2, updated); diff != "" {
		t.Error("Unexpected diff (-want +got):", diff)
	}
	if diff := cmp.Diff(ci, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileTargetValidRevision(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route", WithRouteLabel(map[string]string{"route": "test-route"}))
	rev := newTestRevision(r.Namespace, "revision")
	tc := traffic.Config{Targets: map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "revision",
				Percent:      ptr.Int64(100),
			},
			Active: true,
		}}}}

	ctx = config.ToContext(ctx, &config.Config{
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Minute,
		},
	})

	fakeservingclient.Get(ctx).ServingV1().Revisions(r.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// Get timestamp before reconciling, so that we can compare this to the last pinned timestamp
	// after reconciliation
	beforeTimestamp, err := getLastPinnedTimestamp(t, rev)
	if err != nil {
		t.Fatal("Error getting last pinned:", err)
	}

	if err = reconciler.reconcileTargetRevisions(ctx, &tc, r); err != nil {
		t.Fatal("Error reconciling target revisions:", err)
	}

	// Verify last pinned annotation is updated correctly
	newRev, err := fakeservingclient.Get(ctx).ServingV1().Revisions(r.Namespace).Get(ctx, rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Error getting revision:", err)
	}
	afterTimestamp, err := getLastPinnedTimestamp(t, newRev)
	if err != nil {
		t.Fatal("Error getting last pinned timestamps:", err)
	}
	if beforeTimestamp == afterTimestamp {
		t.Fatal("The last pinned timestamp is not updated")
	}
}

func TestReconcileRevisionTargetDoesNotExist(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route", WithRouteLabel(map[string]string{"route": "test-route"}))
	rev := newTestRevision(r.Namespace, "revision")
	tcInvalidRev := traffic.Config{Targets: map[string]traffic.RevisionTargets{
		traffic.DefaultTarget: {{
			TrafficTarget: v1.TrafficTarget{
				RevisionName: "invalid-revision",
				Percent:      ptr.Int64(100),
			},
			Active: true,
		}}}}
	ctx = config.ToContext(ctx, &config.Config{
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Minute,
		},
	})
	fakeservingclient.Get(ctx).ServingV1().Revisions(r.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)

	// Try reconciling target revisions for a revision that does not exist. No err should be returned
	if err := reconciler.reconcileTargetRevisions(ctx, &tcInvalidRev, r); err != nil {
		t.Fatal("Error reconciling target revisions:", err)
	}
}

func newTestRevision(namespace, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				serving.RevisionLastPinnedAnnotationKey: v1.RevisionLastPinnedString(time.Now().Add(-1 * time.Hour)),
			},
		},
		Spec: v1.RevisionSpec{},
	}
}

func getLastPinnedTimestamp(t *testing.T, rev *v1.Revision) (string, error) {
	lastPinnedTime, ok := rev.Annotations[serving.RevisionLastPinnedAnnotationKey]
	if !ok {
		return "", errors.New("last pinned annotation not found")
	}
	return lastPinnedTime, nil
}

func newTestIngress(t *testing.T, r *v1.Route, trafficOpts ...func(tc *traffic.Config)) *netv1alpha1.Ingress {
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

	tls := []netv1alpha1.IngressTLS{{
		Hosts:           []string{"test-route.test-ns.example.com"},
		SecretName:      "test-secret",
		SecretNamespace: "test-ns",
	}}
	ingress, err := resources.MakeIngress(getContext(), r, tc, tls, "foo-ingress")
	if err != nil {
		t.Error("Unexpected error", err)
	}
	return ingress
}

func TestReconcileCertificatesInsert(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")
	certificate := newCerts([]string{"*.default.example.com"}, r)
	if err := reconciler.reconcileCertificate(ctx, r, certificate); err != nil {
		t.Error("Unexpected error:", err)
	}
	created := getCertificateFromClient(ctx, t, certificate)
	if diff := cmp.Diff(certificate, created); diff != "" {
		t.Error("Unexpected diff (-want +got):", diff)
	}
}

func TestReconcileCertificateUpdate(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")
	certificate := newCerts([]string{"old.example.com"}, r)
	if err := reconciler.reconcileCertificate(ctx, r, certificate); err != nil {
		t.Error("Unexpected error:", err)
	}

	storedCert := getCertificateFromClient(ctx, t, certificate)
	fakecertinformer.Get(ctx).Informer().GetIndexer().Add(storedCert)

	newCertificate := newCerts([]string{"new.example.com"}, r)
	if err := reconciler.reconcileCertificate(ctx, r, newCertificate); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getCertificateFromClient(ctx, t, newCertificate)
	if diff := cmp.Diff(newCertificate, updated); diff != "" {
		t.Error("Unexpected diff (-want +got):", diff)
	}
	if diff := cmp.Diff(certificate, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileIngressClassAnnotation(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	const expClass = "foo.ingress.networking.knative.dev"

	r := Route("test-ns", "test-route")
	ci := newTestIngress(t, r)
	if _, err := reconciler.reconcileIngress(ctx, r, ci); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	fakeciinformer.Get(ctx).Informer().GetIndexer().Add(updated)

	ci2 := newTestIngress(t, r)
	// Add ingress.class annotation.
	ci2.Annotations[networking.IngressClassAnnotationKey] = expClass

	if _, err := reconciler.reconcileIngress(ctx, r, ci2); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated = getRouteIngressFromClient(ctx, t, r)
	updatedClass := updated.Annotations[networking.IngressClassAnnotationKey]
	if expClass != updatedClass {
		t.Errorf("Unexpected annotation got %q want %q", expClass, updatedClass)
	}
}

func newCerts(dnsNames []string, r *v1.Route) *netv1alpha1.Certificate {
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
