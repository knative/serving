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
	"hash/adler32"
	"testing"
	"time"

	// Inject our fakes
	fakesharedclient "knative.dev/pkg/client/injection/client/fake"
	_ "knative.dev/pkg/client/injection/informers/istio/v1alpha3/gateway/fake"
	_ "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice/fake"
	fakekubeclient "knative.dev/pkg/injection/clients/kubeclient/fake"
	_ "knative.dev/pkg/injection/informers/kubeinformers/corev1/pod/fake"
	_ "knative.dev/pkg/injection/informers/kubeinformers/corev1/secret/fake"
	_ "knative.dev/pkg/injection/informers/kubeinformers/corev1/service/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress/fake"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/pkg/kmeta"

	"knative.dev/pkg/apis"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	ing "knative.dev/serving/pkg/reconciler/ingress"
	"knative.dev/serving/pkg/reconciler/ingress/config"
	"knative.dev/serving/pkg/reconciler/ingress/resources"
	presources "knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
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
	defaultMaxRevisionTimeout = time.Duration(apiconfig.DefaultMaxRevisionTimeoutSeconds) * time.Second
	selector                  = map[string]string{
		"istio": "ingressgateway",
	}

	labels = map[string]string{
		networking.IngressLabelKey: "reconciling-clusteringress",
	}

	trueVal  = true
	ownerRef = metav1.OwnerReference{
		APIVersion:         "networking.internal.knative.dev/v1alpha1",
		Kind:               "ClusterIngress",
		Name:               "reconciling-clusteringress",
		Controller:         &trueVal,
		BlockOwnerDeletion: &trueVal,
	}
)

var (
	ingressRules = []v1alpha1.IngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns",
			"test-route.test-ns.svc",
			"test-route.test-ns.svc.cluster.local",
		},
		HTTP: &v1alpha1.HTTPIngressRuleValue{
			Paths: []v1alpha1.HTTPIngressPath{{
				Splits: []v1alpha1.IngressBackendSplit{{
					IngressBackend: v1alpha1.IngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "test-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
				Retries: &v1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
					Attempts:      networking.DefaultRetryCount,
				}},
			},
		},
	}}

	ingressTLS = []v1alpha1.IngressTLS{{
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
		Hosts: []string{"domain.com",
			"test-route.test-ns",
			"test-route.test-ns.svc",
			"test-route.test-ns.svc.cluster.local"},
		Port: v1alpha3.Port{
			Name:     "reconciling-clusteringress:http",
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

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "skip ingress not matching class key",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			addAnnotations(ingress("no-virtualservice-yet", 1234),
				map[string]string{networking.IngressClassAnnotationKey: "fake-controller"}),
		},
	}, {
		Name:                    "create VirtualService matching ClusterIngress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingress("no-virtualservice-yet", 1234),
		},
		WantCreates: []runtime.Object{
			insertProbe(t, resources.MakeMeshVirtualService(ingress("no-virtualservice-yet", 1234))),
			insertProbe(t, resources.MakeIngressVirtualService(ingress("no-virtualservice-yet", 1234),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/knative-ingress-gateway"}, nil))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("no-virtualservice-yet", 1234,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "no-virtualservice-yet-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "no-virtualservice-yet"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "no-virtualservice-yet"),
		},
		Key: "no-virtualservice-yet",
	}, {
		Name:                    "reconcile VirtualService to match desired one",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingress("reconcile-virtualservice", 1234),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice",
					Namespace: system.Namespace(),
					Labels: map[string]string{
						networking.ClusterIngressLabelKey: "reconcile-virtualservice",
						serving.RouteLabelKey:             "test-route",
						serving.RouteNamespaceLabelKey:    "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ingress("reconcile-virtualservice", 1234))},
				},
				Spec: v1alpha3.VirtualServiceSpec{},
			},
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-extra",
					Namespace: system.Namespace(),
					Labels: map[string]string{
						networking.ClusterIngressLabelKey: "reconcile-virtualservice",
						serving.RouteLabelKey:             "test-route",
						serving.RouteNamespaceLabelKey:    "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ingress("reconcile-virtualservice", 1234))},
				},
				Spec: v1alpha3.VirtualServiceSpec{},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: insertProbe(t, resources.MakeIngressVirtualService(ingress("reconcile-virtualservice", 1234),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/knative-ingress-gateway"}, nil))),
		}},
		WantCreates: []runtime.Object{
			insertProbe(t, resources.MakeMeshVirtualService(ingress("reconcile-virtualservice", 1234))),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "test-ns",
				Verb:      "delete",
			},
			Name: "reconcile-virtualservice-extra",
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-virtualservice", 1234,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconcile-virtualservice-mesh"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated VirtualService %s/%s",
				system.Namespace(), "reconcile-virtualservice"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconcile-virtualservice"),
		},
		Key: "reconcile-virtualservice",
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			BaseIngressReconciler: &ing.BaseIngressReconciler{
				Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
				VirtualServiceLister: listers.GetVirtualServiceLister(),
				GatewayLister:        listers.GetGatewayLister(),
				Finalizer:            clusterIngressFinalizer,
				ConfigStore: &testConfigStore{
					config: ReconcilerTestConfig(),
				},
				StatusManager: &fakeStatusManager{
					FakeIsReady: func(service *v1alpha3.VirtualService) (b bool, e error) {
						return true, nil
					},
				},
			},
			clusterIngressLister: listers.GetClusterIngressLister(),
		}
	}))
}

func TestReconcile_EnableAutoTLS(t *testing.T) {
	table := TableTest{{
		Name:                    "Create Ingress Gateway to match newly created ClusterIngress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLS),
			service("istio-system", "istio-ingressgateway", selector),
			// The default shared Gateway which has no servers matching the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of knative-ingress-gateway are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),

			gateway(gatewayName("istio-system/istio-ingressgateway"),
				system.Namespace(), withServers([]v1alpha3.Server{ingressTLSServer, ingressHTTPRedirectServer}),
				withSelector(selector), withLabels(labels), withOwnerReference(ownerRef)),

			insertProbe(t, resources.MakeMeshVirtualService(ingress("reconciling-clusteringress", 1234))),
			insertProbe(t, resources.MakeIngressVirtualService(ingress("reconciling-clusteringress", 1234),
				makeGatewayMap([]string{"knative-testing/knative-ingress-gateway", qualifiedGatewayName("istio-system/istio-ingressgateway")}, nil))),
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
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Gateway %q/%q", system.Namespace(), gatewayName("istio-system/istio-ingressgateway")),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-clusteringress"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "Reconcile with Servers existed in the shared Gateway",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLS),
			service("istio-system", "istio-ingressgateway", selector),
			// The default shared Gateway which has servers matching the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer, ingressTLSServer})),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of knative-ingress-gateway are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer, ingressTLSServer})),

			gateway(gatewayName("istio-system/istio-ingressgateway"),
				system.Namespace(), withServers([]v1alpha3.Server{ingressTLSServer, ingressHTTPRedirectServer}),
				withSelector(selector), withLabels(labels), withOwnerReference(ownerRef)),

			insertProbe(t, resources.MakeMeshVirtualService(ingress("reconciling-clusteringress", 1234))),
			insertProbe(t, resources.MakeIngressVirtualService(ingress("reconciling-clusteringress", 1234),
				makeGatewayMap([]string{"knative-testing/knative-ingress-gateway", qualifiedGatewayName("istio-system/istio-ingressgateway")}, nil))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// The ingressTLSServer in the shared gateway should be removed.
			Object: gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", system.Namespace(), "knative-ingress-gateway"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Gateway %q/%q", system.Namespace(), gatewayName("istio-system/istio-ingressgateway")),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-clusteringress"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "delete ClusterIngress with Finalizer",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-clusteringress", 1234, ingressTLS, []string{clusterIngressFinalizer}),
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Finalizer should be removed.
			Object: ingressWithFinalizers("reconciling-clusteringress", 1234, ingressTLS, []string{}),
		}},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "TLS Secret is not in the namespace of Istio gateway service",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
			service("istio-system", "istio-ingressgateway", selector),

			// The default shared Gateway which has no servers matching the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
			// The namespace (`knative-serving`) of the origin secret is different
			// from the namespace (`istio-system`) of Istio gateway service.
			originSecret("knative-serving", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),

			gateway(gatewayName("istio-system/istio-ingressgateway"),
				system.Namespace(), withServers([]v1alpha3.Server{*withCredentialName(ingressTLSServer.DeepCopy(), targetSecretName), ingressHTTPRedirectServer}),
				withSelector(selector), withLabels(labels), withOwnerReference(ownerRef)),

			insertProbe(t, resources.MakeMeshVirtualService(ingress("reconciling-clusteringress", 1234))),
			insertProbe(t, resources.MakeIngressVirtualService(ingress("reconciling-clusteringress", 1234),
				makeGatewayMap([]string{"knative-testing/knative-ingress-gateway", qualifiedGatewayName("istio-system/istio-ingressgateway")}, nil))),

			// The secret copy under istio-system.
			secret("istio-system", targetSecretName, map[string]string{
				networking.OriginSecretNameLabelKey:      "secret0",
				networking.OriginSecretNamespaceLabelKey: "knative-serving",
			}),
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
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Created", "Created Gateway %q/%q", system.Namespace(), gatewayName("istio-system/istio-ingressgateway")),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-clusteringress"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "Reconcile Target secret",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-clusteringress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
			service("istio-system", "istio-ingressgateway", selector),

			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
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
		WantCreates: []runtime.Object{
			// The creation of knative-ingress-gateway is triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),

			gateway(gatewayName("istio-system/istio-ingressgateway"),
				system.Namespace(), withServers([]v1alpha3.Server{*withCredentialName(ingressTLSServer.DeepCopy(), targetSecretName), ingressHTTPRedirectServer}),
				withSelector(selector), withLabels(labels), withOwnerReference(ownerRef)),

			insertProbe(t, resources.MakeMeshVirtualService(ingress("reconciling-clusteringress", 1234))),
			insertProbe(t, resources.MakeIngressVirtualService(ingress("reconciling-clusteringress", 1234),
				makeGatewayMap([]string{"knative-testing/knative-ingress-gateway", qualifiedGatewayName("istio-system/istio-ingressgateway")}, nil))),
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
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-clusteringress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Created", "Created Gateway %q/%q", system.Namespace(), gatewayName("istio-system/istio-ingressgateway")),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-clusteringress"),
		},
		Key: "reconciling-clusteringress",
	}, {
		Name:                    "Reconcile with autoTLS but cluster local visibilty, mesh only",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLSClusterLocal("reconciling-clusteringress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of ClusterIngress.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway("knative-ingress-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})),
			insertProbe(t, resources.MakeMeshVirtualService(ingressWithTLSClusterLocal("reconciling-clusteringress", 1234, []v1alpha1.IngressTLS{}))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatusClusterLocal("reconciling-clusteringress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{{MeshOnly: true}},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: network.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-clusteringress-mesh"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-clusteringress"),
		},
		Key: "reconciling-clusteringress",
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {

		// As we use a customized resource name for Gateway CRD (i.e. `gateways`), not the one
		// originally generated by kubernetes code generator (i.e. `gatewaies`), we have to
		// explicitly create gateways when setting up the test per suggestion
		// https://knative.dev/serving/blob/a6852fc3b6cdce72b99c5d578dd64f2e03dabb8b/vendor/k8s.io/client-go/testing/fixture.go#L292
		gateways := getGatewaysFromObjects(listers.GetSharedObjects())
		for _, gateway := range gateways {
			fakesharedclient.Get(ctx).NetworkingV1alpha3().Gateways(gateway.Namespace).Create(gateway)
		}

		return &Reconciler{
			BaseIngressReconciler: &ing.BaseIngressReconciler{
				Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
				VirtualServiceLister: listers.GetVirtualServiceLister(),
				GatewayLister:        listers.GetGatewayLister(),
				SecretLister:         listers.GetSecretLister(),
				ServiceLister:        listers.GetK8sServiceLister(),
				Tracker:              &NullTracker{},
				Finalizer:            clusterIngressFinalizer,
				// Enable reconciling gateway.
				ConfigStore: &testConfigStore{
					config: &config.Config{
						Istio: &config.Istio{
							IngressGateways: []config.Gateway{{
								Namespace:  system.Namespace(),
								Name:       "knative-ingress-gateway",
								ServiceURL: network.GetServiceHostname("istio-ingressgateway", "istio-system"),
							}},
						},
						Network: &network.Config{
							AutoTLS:      true,
							HTTPProtocol: network.HTTPRedirected,
						},
					},
				},
				StatusManager: &fakeStatusManager{
					FakeIsReady: func(service *v1alpha3.VirtualService) (b bool, e error) {
						return true, nil
					},
				},
			},
			clusterIngressLister: listers.GetClusterIngressLister(),
		}
	}))
}

func gatewayName(namespaceName string) string {
	return fmt.Sprintf("reconciling-clusteringress-%d", adler32.Checksum([]byte(namespaceName)))
}

func qualifiedGatewayName(namespaceName string) string {
	return system.Namespace() + "/" + gatewayName(namespaceName)
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

func gateway(name, namespace string, opts ...GatewayOption) *v1alpha3.Gateway {
	gateway := &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range opts {
		opt(gateway)
	}
	return gateway
}

// GatewayOption enables future configuration of a Gateway.
type GatewayOption func(*v1alpha3.Gateway)

func withServers(servers []v1alpha3.Server) GatewayOption {
	return func(g *v1alpha3.Gateway) {
		g.Spec.Servers = servers
	}
}

func withSelector(selector map[string]string) GatewayOption {
	return func(g *v1alpha3.Gateway) {
		g.Spec.Selector = selector
	}
}

func withLabels(labels map[string]string) GatewayOption {
	return func(g *v1alpha3.Gateway) {
		g.Labels = labels
	}
}

func withOwnerReference(owner metav1.OwnerReference) GatewayOption {
	return func(g *v1alpha3.Gateway) {
		g.OwnerReferences = []metav1.OwnerReference{owner}
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

func service(namespace, name string, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
		},
	}
}

func withCredentialName(tlsServer *v1alpha3.Server, credentialName string) *v1alpha3.Server {
	tlsServer.TLS.CredentialName = credentialName
	return tlsServer
}

func ingressTLSWithSecretNamespace(namespace string) []v1alpha1.IngressTLS {
	result := []v1alpha1.IngressTLS{}
	for _, tls := range ingressTLS {
		tls.SecretNamespace = namespace
		result = append(result, tls)
	}
	return result
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

var _ reconciler.ConfigStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Istio: &config.Istio{
			IngressGateways: []config.Gateway{{
				Namespace:  system.Namespace(),
				Name:       "knative-test-gateway",
				ServiceURL: network.GetServiceHostname("test-ingressgateway", "istio-system"),
			}, {
				Namespace:  system.Namespace(),
				Name:       "knative-ingress-gateway",
				ServiceURL: network.GetServiceHostname("istio-ingressgateway", "istio-system"),
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

func ingressWithFinalizers(name string, generation int64, tls []v1alpha1.IngressTLS, finalizers []string) *v1alpha1.ClusterIngress {
	ingress := ingressWithTLS(name, generation, tls)
	ingress.ObjectMeta.Finalizers = finalizers
	t := metav1.NewTime(time.Unix(1e9, 0))
	ingress.ObjectMeta.DeletionTimestamp = &t
	return ingress
}
func ingressWithTLS(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.ClusterIngress {
	return ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{})
}

func ingressWithTLSClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.ClusterIngress {
	ci := ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{}).DeepCopy()
	ci.Spec.Visibility = v1alpha1.IngressVisibilityClusterLocal

	rules := ci.Spec.Rules
	for i, rule := range rules {
		rCopy := rule.DeepCopy()
		rCopy.Visibility = v1alpha1.IngressVisibilityClusterLocal
		rules[i] = *rCopy
	}

	ci.Spec.Rules = rules

	return ci
}

func ingressWithTLSAndStatus(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.ClusterIngress {
	ci := ingressWithStatus(name, generation, status)
	ci.Spec.TLS = tls
	return ci
}

func ingressWithTLSAndStatusClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.ClusterIngress {
	ci := ingressWithTLSClusterLocal(name, generation, tls)
	ci.Status = status
	return ci
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	context.Context,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, informers := SetupFakeContext(t)
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}
	controller := NewController(ctx, configMapWatcher)

	controller.Reconciler.(*Reconciler).StatusManager = &fakeStatusManager{
		FakeIsReady: func(*v1alpha3.VirtualService) (bool, error) {
			return true, nil
		},
	}

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

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return ctx, informers, controller, configMapWatcher
}

func TestGlobalResyncOnUpdateGatewayConfigMap(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, informers, ctrl, watcher := newTestSetup(t)

	ctx, cancel := context.WithCancel(ctx)
	grp := errgroup.Group{}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	servingClient := fakeservingclient.Get(ctx)

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

	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}
	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	ingress := ingressWithStatus("config-update", 1234,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: ""},
				},
			},
			Status: duckv1beta1.Status{
				Conditions: duckv1beta1.Conditions{{
					Type:   v1alpha1.IngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionReady,
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
	ctx, informers, ctrl, watcher := newTestSetup(t)

	ctx, cancel := context.WithCancel(ctx)
	grp := errgroup.Group{}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
	}()

	sharedClient := fakesharedclient.Get(ctx)

	h := NewHooks()

	// Check for Gateway created as a signal that syncHandler ran
	h.OnUpdate(&sharedClient.Fake, "gateways", func(obj runtime.Object) HookResult {
		updatedGateway := obj.(*v1alpha3.Gateway)
		// The expected gateway should include the Istio TLS server.
		expectedGateway := gateway(gatewayName("istio-system/origin"),
			system.Namespace(), withServers([]v1alpha3.Server{ingressTLSServer, ingressHTTPRedirectServer}),
			withSelector(selector), withLabels(labels), withOwnerReference(ownerRef))

		if diff := cmp.Diff(updatedGateway, expectedGateway); diff != "" {
			t.Logf("Unexpected Gateway (-want, +got): %v", diff)
			return HookIncomplete
		}

		return HookComplete
	})

	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}
	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

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
					Type:   v1alpha1.IngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	)

	ingressClient := fakeservingclient.Get(ctx).NetworkingV1alpha1().ClusterIngresses()

	// Create a ingress.
	ingressClient.Create(ingress)

	gatewayClient := sharedClient.NetworkingV1alpha3().Gateways(system.Namespace())
	// Create the shared Gateway
	gatewayClient.Create(gateway("knative-test-gateway", system.Namespace(), withServers([]v1alpha3.Server{irrelevantServer})))
	// Create the ingress Gateway
	gatewayClient.Create(gateway(gatewayName("istio-system/origin"),
		system.Namespace(), withSelector(selector), withLabels(labels), withOwnerReference(ownerRef)))

	// Create origin secret. "ns" namespace is the namespace of ingress gateway service.
	secretClient := fakekubeclient.Get(ctx).CoreV1().Secrets("istio-system")
	secretClient.Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret0",
			Namespace: "istio-system",
			UID:       "123",
		},
	})
	svc := service("istio-system", "origin", selector)
	svcClient := fakekubeclient.Get(ctx).CoreV1().Services("istio-system")
	svcClient.Create(svc)

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

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.String {
	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   sets.NewString(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.NewString(privateGateways...),
	}
}

func insertProbe(t *testing.T, vs *v1alpha3.VirtualService) *v1alpha3.VirtualService {
	t.Helper()
	if _, err := resources.InsertProbe(vs); err != nil {
		t.Errorf("failed to insert probe: %v", err)
	}
	return vs
}

type fakeStatusManager struct {
	FakeIsReady func(*v1alpha3.VirtualService) (bool, error)
}

func (m *fakeStatusManager) IsReady(vs *v1alpha3.VirtualService) (bool, error) {
	return m.FakeIsReady(vs)
}
