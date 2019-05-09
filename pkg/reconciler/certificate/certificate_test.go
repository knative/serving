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
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions"
	"github.com/knative/pkg/apis"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	. "github.com/knative/pkg/logging/testing"
	. "github.com/knative/pkg/reconciler/testing"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/certificate/config"
	"github.com/knative/serving/pkg/reconciler/certificate/resources"
	. "github.com/knative/serving/pkg/reconciler/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

const generation = 23132

var (
	correctDNSNames   = []string{"correct-dns1.example.com", "correct-dns2.example.com"}
	incorrectDNSNames = []string{"incorrect-dns.example.com"}
	notAfter          = &metav1.Time{
		Time: time.Unix(123, 456),
	}
)

var certManagerClient *fakecmclient.Clientset

func TestNewController(t *testing.T) {
	defer ClearAll()

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.CertManagerConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"issuerRef": "kind: ClusterIssuer\nname: letsencrypt-issuer",
		},
	})

	kubeClient := fakekubeclientset.NewSimpleClientset()
	sharedClient := fakesharedclientset.NewSimpleClientset()

	servingClient := fakeclientset.NewSimpleClientset()
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	knCertInformer := servingInformer.Networking().V1alpha1().Certificates()
	certManagerClient := fakecmclient.NewSimpleClientset()
	certManagerInformer := certmanagerinformers.NewSharedInformerFactory(certManagerClient, 0)
	cmCertInforer := certManagerInformer.Certmanager().V1alpha1().Certificates()

	c := NewController(reconciler.Options{
		KubeClientSet:    kubeClient,
		SharedClientSet:  sharedClient,
		ServingClientSet: servingClient,
		ConfigMapWatcher: configMapWatcher,
		Logger:           TestLogger(t),
	}, knCertInformer, cmCertInforer, certManagerClient)

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
		WantCreates: []metav1.Object{
			resources.MakeCertManagerCertificate(certmanagerConfig(), knCert("knCert", "foo")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					Status: duckv1beta1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1beta1.Conditions{{
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
					Status: duckv1beta1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1beta1.Conditions{{
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
		Name: "set Knative Certificate ready status with CM Certificate ready status",
		Objects: []runtime.Object{
			knCert("knCert", "foo"),
			cmCertWithStatus("knCert", "foo", correctDNSNames, certmanagerv1alpha1.ConditionTrue),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: knCertWithStatus("knCert", "foo",
				&v1alpha1.CertificateStatus{
					NotAfter: notAfter,
					Status: duckv1beta1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1beta1.Conditions{{
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
					Status: duckv1beta1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1beta1.Conditions{{
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
					Status: duckv1beta1.Status{
						ObservedGeneration: generation,
						Conditions: duckv1beta1.Conditions{{
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
	cert.UpdateStatusCondition(certmanagerv1alpha1.CertificateConditionReady, status, "", "", false)
	cert.Status.NotAfter = notAfter
	return cert
}
