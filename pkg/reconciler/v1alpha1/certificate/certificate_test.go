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

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	fakecmclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/fake"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/certificate/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/certificate/resources"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
)

const generation = 23132

var (
	corretDNSNames    = []string{"correct-dns1.example.com", "correct-dns2.example.com"}
	incorrectDNSNames = []string{"incorrect-dns.example.com"}
	notAfter          = &metav1.Time{
		Time: time.Unix(123, 456),
	}
)

var certManagerClient *fakecmclient.Clientset

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
		WantCreates: []metav1.Object{
			resources.MakeCertManagerCertificate(certmanagerConfig(), knCert("knCert", "foo")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1alpha1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1alpha1.Conditions{{
							Type:     v1alpha1.CertificateCondidtionReady,
							Status:   corev1.ConditionUnknown,
							Severity: duckv1alpha1.ConditionSeverityError,
						}},
					},
				}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Cert-Manager Certificate %q/%q", "foo", "knCert"),
		},
		Key: "foo/knCert",
	}, {
		Name: "reconcile CM certificate to match desired one",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCert("knCert", "foo", incorrectDNSNames),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cmCert("knCert", "foo", corretDNSNames),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1alpha1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1alpha1.Conditions{{
							Type:     v1alpha1.CertificateCondidtionReady,
							Status:   corev1.ConditionUnknown,
							Severity: duckv1alpha1.ConditionSeverityError,
						}},
					},
				}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Spec for Cert-Manager Certificate %q/%q", "foo", "knCert"),
		},
		Key: "foo/knCert",
	}, {
		Name: "set Knative Certificate status with CM Certificate status",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCertWithStatus("knCert", "foo", corretDNSNames, true),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					NotAfter: notAfter,
					Status: duckv1alpha1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1alpha1.Conditions{{
							Type:     v1alpha1.CertificateCondidtionReady,
							Status:   corev1.ConditionTrue,
							Severity: duckv1alpha1.ConditionSeverityError,
						}},
					},
				}),
		}},
		Key: "foo/knCert",
	}}

	table.Test(t, makeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		certManagerClient = fakecmclient.NewSimpleClientset(listers.GetCMCertificateObjects()...)
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			knCertificateLister: listers.GetKnCertificateLister(),
			cmCertificateLister: listers.GetCMCertificateLister(),
			certManagerClient:   certManagerClient,
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

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

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

func makeFactory(c func(*Listers, reconciler.Options) controller.Reconciler) Factory {
	return func(t *testing.T, r *TableRow) (controller.Reconciler, ActionRecorderList, EventList, *FakeStatsReporter) {
		factory := MakeFactory(c)
		c, actionRecorderList, eventList, statsReporter := factory(t, r)
		// We need add certManagerClient into the actionRecorderList so that
		// any actions of certManagerClient could be recorded and tested.
		actionRecorderList = append(actionRecorderList, certManagerClient)
		return c, actionRecorderList, eventList, statsReporter
	}
}

func knCert(name, namespace string) *v1alpha1.Certificate {
	return knCertWithStatus(name, namespace, &v1alpha1.CertificateStatus{})
}

func knCertWithStatus(name, namespace string, status *v1alpha1.CertificateStatus) *v1alpha1.Certificate {
	return &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   corretDNSNames,
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

func cmCertWithStatus(name, namespace string, dnsNames []string, isReady bool) *certmanagerv1alpha1.Certificate {
	cert := cmCert(name, namespace, dnsNames)
	var conditionStatus certmanagerv1alpha1.ConditionStatus
	if isReady {
		conditionStatus = certmanagerv1alpha1.ConditionTrue
	} else {
		conditionStatus = certmanagerv1alpha1.ConditionFalse
	}
	cert.UpdateStatusCondition(certmanagerv1alpha1.CertificateConditionReady, conditionStatus, "", "", false)
	cert.Status.NotAfter = notAfter
	return cert
}
