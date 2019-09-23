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

package certificate

import (
	"context"
	"testing"
	"time"

	fakecertmanagerclient "knative.dev/serving/pkg/client/certmanager/injection/client/fake"
	_ "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1alpha1/certificate/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/certificate/config"
	"knative.dev/serving/pkg/reconciler/certificate/resources"

	. "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
)

const generation = 23132

var (
	correctDNSNames   = []string{"correct-dns1.example.com", "correct-dns2.example.com"}
	incorrectDNSNames = []string{"incorrect-dns.example.com"}
	notAfter          = &metav1.Time{
		Time: time.Unix(123, 456),
	}
)

func TestNewController(t *testing.T) {
	defer ClearAll()
	ctx, _ := SetupFakeContext(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.CertManagerConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"issuerRef": "kind: ClusterIssuer\nname: letsencrypt-issuer",
		},
	})

	c := NewController(ctx, configMapWatcher)
	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "create CM certificate matching Knative Certificate",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
		},
		WantCreates: []runtime.Object{
			resources.MakeCertManagerCertificate(certmanagerConfig(), knCert("knCert", "foo")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
							Reason:   noCMConditionReason,
							Message:  noCMConditionMessage,
						}},
					},
				}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Cert-Manager Certificate %s/%s", "foo", "knCert"),
		},
		Key: "foo/knCert",
	}, {
		Name: "reconcile CM certificate to match desired one",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCert("knCert", "foo", incorrectDNSNames),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cmCert("knCert", "foo", correctDNSNames),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
							Reason:   noCMConditionReason,
							Message:  noCMConditionMessage,
						}},
					},
				}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Spec for Cert-Manager Certificate %s/%s", "foo", "knCert"),
		},
		Key: "foo/knCert",
	}, {
		Name: "observed generation is still updated when error is encountered, and ready status is unknown",
		Objects: []runtime.Object{
			knCertWithStatusAndGeneration("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1.Status{
						ObservedGeneration: generation + 1,
						Conditions: duckv1.Conditions{{
							Type:   v1alpha1.CertificateConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}, generation+1),
			cmCert("knCert", "foo", incorrectDNSNames),
		},
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "certificates"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cmCert("knCert", "foo", correctDNSNames),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatusAndGeneration("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1.Status{
						ObservedGeneration: generation + 1,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
							Reason:   notReconciledReason,
							Message:  notReconciledMessage,
						}},
					},
				}, generation+1),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to create Cert-Manager Certificate %s: %v",
				"foo/knCert", "inducing failure for update certificates"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update Cert-Manager Certificate: inducing failure for update certificates"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Certificate %s: %v",
				"foo/knCert", "inducing failure for update certificates"),
		},
		Key: "foo/knCert",
	}, {
		Name: "set Knative Certificate ready status with CM Certificate ready status",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCertWithStatus("knCert", "foo", correctDNSNames, certmanagerv1alpha1.ConditionTrue),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					NotAfter: notAfter,
					Status: duckv1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				}),
		}},
		Key: "foo/knCert",
	}, {
		Name: "set Knative Certificate unknown status with CM Certificate unknown status",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCertWithStatus("knCert", "foo", correctDNSNames, certmanagerv1alpha1.ConditionUnknown),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					NotAfter: notAfter,
					Status: duckv1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
						}},
					},
				}),
		}},
		Key: "foo/knCert",
	}, {
		Name: "set Knative Certificate not ready status with CM Certificate not ready status",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCertWithStatus("knCert", "foo", correctDNSNames, certmanagerv1alpha1.ConditionFalse),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					NotAfter: notAfter,
					Status: duckv1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.CertificateConditionReady,
							Status:   corev1.ConditionFalse,
							Severity: apis.ConditionSeverityError,
						}},
					},
				}),
		}},
		Key: "foo/knCert",
	}}

	defer ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			knCertificateLister: listers.GetKnCertificateLister(),
			cmCertificateLister: listers.GetCMCertificateLister(),
			certManagerClient:   fakecertmanagerclient.Get(ctx),
			configStore: &testConfigStore{
				config: &config.Config{
					CertManager: certmanagerConfig(),
				},
			},
		}
	}))
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ reconciler.ConfigStore = (*testConfigStore)(nil)

func certmanagerConfig() *config.CertManagerConfig {
	return &config.CertManagerConfig{
		SolverConfig: &certmanagerv1alpha1.SolverConfig{
			DNS01: &certmanagerv1alpha1.DNS01SolverConfig{
				Provider: "cloud-dns-provider",
			},
		},
		IssuerRef: &certmanagerv1alpha1.ObjectReference{
			Kind: "ClusterIssuer",
			Name: "Letsencrypt-issuer",
		},
	}
}

func knCert(name, namespace string) *v1alpha1.Certificate {
	return knCertWithStatus(name, namespace, &v1alpha1.CertificateStatus{})
}

func knCertWithStatus(name, namespace string, status *v1alpha1.CertificateStatus) *v1alpha1.Certificate {
	return knCertWithStatusAndGeneration(name, namespace, status, generation)
}

func knCertWithStatusAndGeneration(name, namespace string, status *v1alpha1.CertificateStatus, gen int) *v1alpha1.Certificate {
	return &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: int64(gen),
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   correctDNSNames,
			SecretName: "secret0",
		},
		Status: *status,
	}
}

func cmCert(name, namespace string, dnsNames []string) *certmanagerv1alpha1.Certificate {
	cert := resources.MakeCertManagerCertificate(certmanagerConfig(), knCert(name, namespace))
	cert.Spec.DNSNames = dnsNames
	return cert
}

func cmCertWithStatus(name, namespace string, dnsNames []string, status certmanagerv1alpha1.ConditionStatus) *certmanagerv1alpha1.Certificate {
	cert := cmCert(name, namespace, dnsNames)
	cert.Status.Conditions = []certmanagerv1alpha1.CertificateCondition{{
		Type:   certmanagerv1alpha1.CertificateConditionReady,
		Status: status,
	}}
	cert.Status.NotAfter = notAfter
	return cert
}
