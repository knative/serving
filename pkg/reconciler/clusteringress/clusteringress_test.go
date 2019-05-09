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

package clusteringress

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/knative/pkg/kmeta"

	"github.com/google/go-cmp/cmp"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	"github.com/knative/pkg/apis"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	sharedinformers "github.com/knative/pkg/client/informers/externalversions"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/system"
	_ "github.com/knative/pkg/system/testing"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/clusteringress/config"
	"github.com/knative/serving/pkg/reconciler/clusteringress/resources"
	presources "github.com/knative/serving/pkg/resources"

	. "github.com/knative/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

const (
	originDomainInternal = "origin.istio-system.svc.cluster.local"
	newDomainInternal    = "custom.istio-system.svc.cluster.local"
	targetSecretName     = "reconciling-clusteringress-uid"
)

var (
	originGateways = map[string]string{
		"gateway.knative-test-gateway": originDomainInternal,
	}
	newGateways = map[string]string{
		"gateway.knative-ingress-gateway": newDomainInternal,
		"gateway.knative-test-gateway":    originDomainInternal,
	}
)

var (
	ingressRules = []v1alpha1.ClusterIngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
		},
		HTTP: &v1alpha1.HTTPClusterIngressRuleValue{
			Paths: []v1alpha1.HTTPClusterIngressPath{{
				Splits: []v1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "test-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: networking.DefaultTimeout},
				Retries: &v1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: networking.DefaultTimeout},
					Attempts:      networking.DefaultRetryCount,
				}},
			},
		},
	}}

	ingressTLS = []v1alpha1.ClusterIngressTLS{{
		Hosts:             []string{"host-tls.example.com"},
		SecretName:        "secret0",
		SecretNamespace:   "istio-system",
		ServerCertificate: "tls.crt",
		PrivateKey:        "tls.key",
	}}

	// The gateway server according to ingressTLS.
	ingressTLSServer = v1alpha3.Server{
		Hosts: []string{"host-tls.example.com"},
		Port: v1alpha3.Port{
			Name:     "reconciling-clusteringress:0",
			Number:   443,
			Protocol: v1alpha3.ProtocolHTTPS,
		},
		TLS: &v1alpha3.TLSOptions{
			Mode:              v1alpha3.TLSModeSimple,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "secret0",
		},
	}

	ingressHTTPRedirectServer = v1alpha3.Server{
		Hosts: []string{"*"},
		Port: v1alpha3.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: v1alpha3.ProtocolHTTP,
		},
		TLS: &v1alpha3.TLSOptions{
			HTTPSRedirect: true,
		},
	}

	// The gateway server irrelevant to ingressTLS.
	irrelevantServer = v1alpha3.Server{
		Hosts: []string{"test.example.com"},
		Port: v1alpha3.Port{
			Name:     "test:0",
			Number:   443,
			Protocol: v1alpha3.ProtocolHTTPS,
		},
		TLS: &v1alpha3.TLSOptions{
			Mode:              v1alpha3.TLSModeSimple,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "other-secret",
		},
	}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile_EnableAutoTLS(t *testing.T) {
	table := TableTest{{
		Name:                    "update Gateway to match newly created ClusterIngress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer}),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []metav1.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer}),

			resources.MakeVirtualService(ingress("reconciling-clusteringress", 1234),
				[]string{"knative-ingress-gateway"}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// ingressTLSServer needs to be added into Gateway.
			Object: gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{ingressTLSServer, irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-clusteringress", clusterIngressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", system.Namespace(), "knative-ingress-gateway"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "No preinstalled Gateways",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLS),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []metav1.Object{
			resources.MakeVirtualService(ingress("reconciling-clusteringress", 1234),
				[]string{"knative-ingress-gateway"}),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-clusteringress", clusterIngressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeWarning, "InternalError", `gateway.networking.istio.io "knative-ingress-gateway" not found`),
		},
		// Error should be returned when there is no preinstalled gateways.
		WantErr: true,
		Key:     "reconciling-clusteringress",
	}, {
		Name:                    "delete ClusterIngress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-clusteringress", 1234, ingressTLS, []string{clusterIngressFinalizer}),
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer, ingressTLSServer}),
		},
		WantCreates: []metav1.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer, ingressTLSServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer}),
		}, {
			// Finalizer should be removed.
			Object: ingressWithFinalizers("reconciling-clusteringress", 1234, ingressTLS, []string{}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", system.Namespace(), "knative-ingress-gateway"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "TLS Secret is not in the namespace of Istio gateway service",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
			// No Gateway servers match the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer}),
			// The namespace (`knative-serving`) of the origin secret is different
			// from the namespace (`istio-system`) of Istio gateway service.
			originSecret("knative-serving", "secret0"),
		},
		WantCreates: []metav1.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{irrelevantServer}),

			resources.MakeVirtualService(ingress("reconciling-clusteringress", 1234),
				[]string{"knative-ingress-gateway"}),

			// The secret copy under istio-system.
			secret("istio-system", targetSecretName, map[string]string{
				networking.OriginSecretNameLabelKey:      "secret0",
				networking.OriginSecretNamespaceLabelKey: "knative-serving",
			}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// ingressTLSServer with the name of the secret copy needs to be added into Gateway.
			Object: gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{*withCredentialName(ingressTLSServer.DeepCopy(), targetSecretName), irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-clusteringress", clusterIngressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", system.Namespace(), "knative-ingress-gateway"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "Reconcile Target secret",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLSWithSecretNamespace("knative-serving")),

			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{*withCredentialName(ingressTLSServer.DeepCopy(), targetSecretName), irrelevantServer}),
			// The origin secret.
			originSecret("knative-serving", "secret0"),

			// The target secret that has the Data different from the origin secret. The Data should be reconciled.
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: "istio-system",
					Labels: map[string]string{
						networking.OriginSecretNameLabelKey:      "secret0",
						networking.OriginSecretNamespaceLabelKey: "knative-serving",
					},
				},
				Data: map[string][]byte{
					"wrong_data": []byte("wrongdata"),
				},
			},
		},
		WantCreates: []metav1.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), []v1alpha3.Server{*withCredentialName(ingressTLSServer.DeepCopy(), targetSecretName), irrelevantServer}),

			resources.MakeVirtualService(ingress("reconciling-clusteringress", 1234),
				[]string{"knative-ingress-gateway"}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: "istio-system",
					Labels: map[string]string{
						networking.OriginSecretNameLabelKey:      "secret0",
						networking.OriginSecretNamespaceLabelKey: "knative-serving",
					},
				},
				// The data is expected to be updated to the right one.
				Data: map[string][]byte{
					"test-secret": []byte("abcd"),
				},
			},
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-clusteringress", clusterIngressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.ClusterIngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", "istio-system", targetSecretName),
		},
		Key: "reconciling-clusteringress",
	}}
	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {

		// As we use a customized resource name for Gateway CRD (i.e. `gateways`), not the one
		// originally generated by kubernetes code generator (i.e. `gatewaies`), we have to
		// explicitly create gateways when setting up the test per suggestion
		// https://github.com/knative/serving/blob/a6852fc3b6cdce72b99c5d578dd64f2e03dabb8b/vendor/k8s.io/client-go/testing/fixture.go#L292
		gateways := getGatewaysFromObjects(listers.GetSharedObjects())
		for _, gateway := range gateways {
			opt.SharedClientSet.NetworkingV1alpha3().Gateways(gateway.Namespace).Create(gateway)
		}

		return &Reconciler{
			Base:                 reconciler.NewBase(opt, controllerAgentName),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			clusterIngressLister: listers.GetClusterIngressLister(),
			gatewayLister:        listers.GetGatewayLister(),
			secretLister:         listers.GetSecretLister(),
			tracker:              &NullTracker{},
			// Enable reconciling gateway.
			configStore: &testConfigStore{
				config: &config.Config{
					Istio: &config.Istio{
						IngressGateways: []config.Gateway{{
							GatewayName: "knative-ingress-gateway",
							ServiceURL:  network.GetServiceHostname("istio-ingressgateway", "istio-system"),
						}},
					},
					Network: &network.Config{
						AutoTLS:      true,
						HTTPProtocol: network.HTTPDisabled,
					},
				},
			},
		}
	}))
}

func getGatewaysFromObjects(objects []runtime.Object) []*v1alpha3.Gateway {
	gateways := []*v1alpha3.Gateway{}
	for _, object := range objects {
		if gateway, ok := object.(*v1alpha3.Gateway); ok {
			gateways = append(gateways, gateway)
		}
	}
	return gateways
}

func gateway(name, namespace string, servers []v1alpha3.Server) *v1alpha3.Gateway {
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha3.GatewaySpec{
			Servers: servers,
		},
	}
}

func originSecret(namespace, name string) *corev1.Secret {
	tmp := secret(namespace, name, map[string]string{})
	tmp.UID = "uid"
	return tmp
}

func secret(namespace, name string, labels map[string]string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ingress("reconciling-clusteringress", 1234))},
		},
		Data: map[string][]byte{
			"test-secret": []byte("abcd"),
		},
	}
}

func withCredentialName(tlsServer *v1alpha3.Server, credentialName string) *v1alpha3.Server {
	tlsServer.TLS.CredentialName = credentialName
	return tlsServer
}

func ingressTLSWithSecretNamespace(namespace string) []v1alpha1.ClusterIngressTLS {
	result := []v1alpha1.ClusterIngressTLS{}
	for _, tls := range ingressTLS {
		tls.SecretNamespace = namespace
		result = append(result, tls)
	}
	return result
}

func patchAddFinalizerAction(ingressName, finalizer string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{
		Name: ingressName,
	}
	patch := fmt.Sprintf(`{"metadata":{"finalizers":["%s"],"resourceVersion":"v1"}}`, finalizer)
	action.Patch = []byte(patch)
	return action
}

func addAnnotations(ing *v1alpha1.ClusterIngress, annos map[string]string) *v1alpha1.ClusterIngress {
	ing.ObjectMeta.Annotations = presources.UnionMaps(annos, ing.ObjectMeta.Annotations)
	return ing
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Istio: &config.Istio{
			IngressGateways: []config.Gateway{{
				GatewayName: "knative-test-gateway",
				ServiceURL:  network.GetServiceHostname("test-ingressgateway", "istio-system"),
			}, {
				GatewayName: "knative-ingress-gateway",
				ServiceURL:  network.GetServiceHostname("istio-ingressgateway", "istio-system"),
			}},
		},
		Network: &network.Config{
			AutoTLS: false,
		},
	}
}

func ingressWithStatus(name string, generation int64, status v1alpha1.IngressStatus) *v1alpha1.ClusterIngress {
	return &v1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.IngressSpec{
			DeprecatedGeneration: generation,
			Rules:                ingressRules,
		},
		Status: status,
	}
}

func ingress(name string, generation int64) *v1alpha1.ClusterIngress {
	return ingressWithStatus(name, generation, v1alpha1.IngressStatus{})
}

func ingressWithFinalizers(name string, generation int64, tls []v1alpha1.ClusterIngressTLS, finalizers []string) *v1alpha1.ClusterIngress {
	ingress := ingressWithTLS(name, generation, tls)
	ingress.ObjectMeta.Finalizers = finalizers
	t := metav1.NewTime(time.Unix(1e9, 0))
	ingress.ObjectMeta.DeletionTimestamp = &t
	return ingress
}
func ingressWithTLS(name string, generation int64, tls []v1alpha1.ClusterIngressTLS) *v1alpha1.ClusterIngress {
	return ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{})
}

func ingressWithTLSAndStatus(name string, generation int64, tls []v1alpha1.ClusterIngressTLS, status v1alpha1.IngressStatus) *v1alpha1.ClusterIngress {
	ci := ingressWithStatus(name, generation, status)
	ci.Spec.TLS = tls
	return ci
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	sharedClient *fakesharedclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *controller.Impl,
	rclr *Reconciler,
	kubeInformer kubeinformers.SharedInformerFactory,
	sharedInformer sharedinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher *configmap.ManualWatcher) {

	kubeClient = fakekubeclientset.NewSimpleClientset()
	cms := append([]*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.IstioConfigName,
			Namespace: system.Namespace(),
		},
		Data: originGateways,
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"autoTLS": "Disabled",
		},
	}}, configs...)

	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}
	sharedClient = fakesharedclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	sharedInformer = sharedinformers.NewSharedInformerFactory(sharedClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)

	controller = NewController(
		reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           logtesting.TestLogger(t),
		},
		servingInformer.Networking().V1alpha1().ClusterIngresses(),
		sharedInformer.Networking().V1alpha3().VirtualServices(),
		sharedInformer.Networking().V1alpha3().Gateways(),
		kubeInformer.Core().V1().Secrets(),
	)

	rclr = controller.Reconciler.(*Reconciler)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return
}

func TestGlobalResyncOnUpdateGatewayConfigMap(t *testing.T) {
	defer logtesting.ClearAll()
	_, _, servingClient, controller, _, _, sharedInformer, servingInformer, watcher := newTestSetup(t)

	stopCh := make(chan struct{})
	grp := errgroup.Group{}
	defer func() {
		close(stopCh)
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	h := NewHooks()

	// Check for ClusterIngress created as a signal that syncHandler ran
	h.OnUpdate(&servingClient.Fake, "clusteringresses", func(obj runtime.Object) HookResult {
		ci := obj.(*v1alpha1.ClusterIngress)
		t.Logf("clusteringress updated: %q", ci.Name)

		gateways := ci.Status.LoadBalancer.Ingress
		if len(gateways) != 1 {
			t.Logf("Unexpected gateways: %v", gateways)
			return HookIncomplete
		}
		expectedDomainInternal := newDomainInternal
		if gateways[0].DomainInternal != expectedDomainInternal {
			t.Logf("Expected gateway %q but got %q", expectedDomainInternal, gateways[0].DomainInternal)
			return HookIncomplete
		}

		return HookComplete
	})

	servingInformer.Start(stopCh)
	sharedInformer.Start(stopCh)
	if err := watcher.Start(stopCh); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	servingInformer.WaitForCacheSync(stopCh)
	sharedInformer.WaitForCacheSync(stopCh)

	grp.Go(func() error { return controller.Run(1, stopCh) })

	ingress := ingressWithStatus("config-update", 1234,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: ""},
				},
			},
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   v1alpha1.ClusterIngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.ClusterIngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.ClusterIngressConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	)
	ingressClient := servingClient.NetworkingV1alpha1().ClusterIngresses()

	// Create a ingress.
	ingressClient.Create(ingress)

	// Test changes in gateway config map. ClusterIngress should get updated appropriately.
	domainConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.IstioConfigName,
			Namespace: system.Namespace(),
		},
		Data: newGateways,
	}
	watcher.OnChange(&domainConfig)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestGlobalResyncOnUpdateNetwork(t *testing.T) {
	defer logtesting.ClearAll()
	kubeClient, sharedClient, servingClient, controller, _, kubeInformer, sharedInformer, servingInformer, watcher := newTestSetup(t)

	stopCh := make(chan struct{})
	grp := errgroup.Group{}
	defer func() {
		close(stopCh)
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	h := NewHooks()

	// Check for Gateway created as a signal that syncHandler ran
	h.OnUpdate(&sharedClient.Fake, "gateways", func(obj runtime.Object) HookResult {
		updatedGateway := obj.(*v1alpha3.Gateway)
		// The expected gateway should include the Istio TLS server.
		expectedGateway := gateway("knative-test-gateway", system.Namespace(), []v1alpha3.Server{ingressTLSServer})
		expectedGateway.Spec.Servers = append(expectedGateway.Spec.Servers, ingressHTTPRedirectServer)
		expectedGateway.Spec.Servers = resources.SortServers(expectedGateway.Spec.Servers)

		if diff := cmp.Diff(updatedGateway, expectedGateway); diff != "" {
			t.Logf("Unexpected Gateway (-want, +got): %v", diff)
			return HookIncomplete
		}

		return HookComplete
	})

	servingInformer.Start(stopCh)
	sharedInformer.Start(stopCh)
	kubeInformer.Start(stopCh)
	if err := watcher.Start(stopCh); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	servingInformer.WaitForCacheSync(stopCh)
	sharedInformer.WaitForCacheSync(stopCh)
	kubeInformer.WaitForCacheSync(stopCh)

	grp.Go(func() error { return controller.Run(1, stopCh) })

	ingress := ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
		ingressTLS,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: originDomainInternal},
				},
			},
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   v1alpha1.ClusterIngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.ClusterIngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.ClusterIngressConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	)

	ingressClient := servingClient.NetworkingV1alpha1().ClusterIngresses()

	// Create a ingress.
	ingressClient.Create(ingress)

	gatewayClient := sharedClient.NetworkingV1alpha3().Gateways(system.Namespace())
	// Create a Gateway
	gatewayClient.Create(gateway("knative-test-gateway", system.Namespace(), []v1alpha3.Server{}))

	// Create origin secret. "ns" namespace is the namespace of ingress gateway service.
	secretClient := kubeClient.CoreV1().Secrets("istio-system")
	secretClient.Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret0",
			Namespace: "istio-system",
			UID:       "123",
		},
	})

	// Test changes in autoTLS of config-network ConfigMap. ClusterIngress should get updated appropriately.
	networkConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"autoTLS":      "Enabled",
			"httpProtocol": "Redirected",
		},
	}
	watcher.OnChange(&networkConfig)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}
