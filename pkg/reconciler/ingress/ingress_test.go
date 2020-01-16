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

package ingress

import (
	"context"
	"fmt"
	"testing"
	"time"

	// Inject our fakes
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	fakeistioclient "knative.dev/serving/pkg/client/istio/injection/client/fake"
	_ "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/gateway/fake"
	_ "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/virtualservice/fake"
	"knative.dev/serving/pkg/network/ingress"

	proto "github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/pkg/kmeta"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"

	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/ingress/config"
	"knative.dev/serving/pkg/reconciler/ingress/resources"
	presources "knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
)

const (
	originDomainInternal = "origin.istio-system.svc.cluster.local"
	newDomainInternal    = "custom.istio-system.svc.cluster.local"
	targetSecretName     = "reconciling-ingress-uid"
)

var (
	originGateways = map[string]string{
		"gateway.knative-test-gateway": originDomainInternal,
	}
	newGateways = map[string]string{
		"gateway." + networking.KnativeIngressGateway: newDomainInternal,
		"gateway.knative-test-gateway":                originDomainInternal,
	}
	ingressGateway = map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString(networking.KnativeIngressGateway),
	}
	gateways = map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString("knative-test-gateway", networking.KnativeIngressGateway),
	}
	defaultMaxRevisionTimeout = time.Duration(apiconfig.DefaultMaxRevisionTimeoutSeconds) * time.Second
)

var (
	ingressRules = []v1alpha1.IngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
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
		Hosts:           []string{"host-tls.example.com"},
		SecretName:      "secret0",
		SecretNamespace: "istio-system",
	}}

	// The gateway server according to ingressTLS.
	ingressTLSServer = &istiov1alpha3.Server{
		Hosts: []string{"host-tls.example.com"},
		Port: &istiov1alpha3.Port{
			Name:     "test-ns/reconciling-ingress:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.Server_TLSOptions{
			Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "secret0",
		},
	}

	ingressHTTPRedirectServer = &istiov1alpha3.Server{
		Hosts: []string{"*"},
		Port: &istiov1alpha3.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
		Tls: &istiov1alpha3.Server_TLSOptions{
			HttpsRedirect: true,
		},
	}

	// The gateway server irrelevant to ingressTLS.
	irrelevantServer = &istiov1alpha3.Server{
		Hosts: []string{"test.example.com"},
		Port: &istiov1alpha3.Port{
			Name:     "test:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.Server_TLSOptions{
			Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "other-secret",
		},
	}

	deletionTime = metav1.NewTime(time.Unix(1e9, 0))
)

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "skip ingress not matching class key",
		Objects: []runtime.Object{
			addAnnotations(ing("no-virtualservice-yet", 1234),
				map[string]string{networking.IngressClassAnnotationKey: "fake-controller"}),
		},
	}, {
		Name: "create VirtualService matching Ingress",

		Objects: []runtime.Object{
			ing("no-virtualservice-yet", 1234),
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ing("no-virtualservice-yet", 1234)), gateways),
			resources.MakeIngressVirtualService(insertProbe(ing("no-virtualservice-yet", 1234)),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + networking.KnativeIngressGateway}, nil)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("no-virtualservice-yet", 1234,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
		Key: "test-ns/no-virtualservice-yet",
	}, {
		Name:    "observed generation is updated when error is encountered in reconciling, and ingress ready status is unknown",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "virtualservices"),
		},
		Objects: []runtime.Object{
			ingressWithStatus("reconcile-failed", 1234,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-failed",
					Namespace: "test-ns",
					Labels: map[string]string{
						networking.IngressLabelKey:     "reconcile-failed",
						serving.RouteLabelKey:          "test-route",
						serving.RouteNamespaceLabelKey: "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-failed", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ing("reconcile-failed", 1234)), gateways),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(insertProbe(ing("reconcile-failed", 1234)),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + networking.KnativeIngressGateway}, nil)),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-failed", 1234,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Reason:   virtualServiceNotReconciled,
							Severity: apis.ConditionSeverityError,
							Message:  "failed to update VirtualService: inducing failure for update virtualservices",
							Status:   corev1.ConditionFalse,
						}, {
							Type:   v1alpha1.IngressConditionNetworkConfigured,
							Status: corev1.ConditionTrue,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionFalse,
							Severity: apis.ConditionSeverityError,
							Reason:   notReconciledReason,
							Message:  notReconciledMessage,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconcile-failed-mesh"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update VirtualService: inducing failure for update virtualservices"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconcile-failed"),
		},
		Key: "test-ns/reconcile-failed",
	}, {
		Name: "reconcile VirtualService to match desired one",
		Objects: []runtime.Object{
			ing("reconcile-virtualservice", 1234),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice",
					Namespace: "test-ns",
					Labels: map[string]string{
						networking.IngressLabelKey:     "reconcile-virtualservice",
						serving.RouteLabelKey:          "test-route",
						serving.RouteNamespaceLabelKey: "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-extra",
					Namespace: "test-ns",
					Labels: map[string]string{
						networking.IngressLabelKey:     "reconcile-virtualservice",
						serving.RouteLabelKey:          "test-route",
						serving.RouteNamespaceLabelKey: "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(insertProbe(ing("reconcile-virtualservice", 1234)),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + networking.KnativeIngressGateway}, nil)),
		}},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ing("reconcile-virtualservice", 1234)), gateways),
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
							{DomainInternal: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
				"test-ns", "reconcile-virtualservice"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconcile-virtualservice"),
		},
		Key: "test-ns/reconcile-virtualservice",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			finalizer:            ingressFinalizer,
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(),
			},
			statusManager: &fakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			},
			ingressLister: listers.GetIngressLister(),
		}
	}))
}

func TestReconcile_EnableAutoTLS(t *testing.T) {
	table := TableTest{{
		Name:                    "update Gateway to match newly created Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of Ingress.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				makeGatewayMap([]string{"knative-testing/" + networking.KnativeIngressGateway}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// ingressTLSServer needs to be added into Gateway.
			Object: gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{ingressTLSServer, irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), networking.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name: "No preinstalled Gateways",
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				makeGatewayMap([]string{"knative-testing/" + networking.KnativeIngressGateway}, nil)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
							Reason:   notReconciledReason,
							Message:  notReconciledMessage,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress"),
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to get Gateway: gateway.networking.istio.io "%s" not found`, networking.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		// Error should be returned when there is no preinstalled gateways.
		WantErr: true,
		Key:     "test-ns/reconciling-ingress",
	}, {
		Name:                    "delete Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-ingress", 1234, ingressTLS, []string{ingressFinalizer}, &deletionTime),
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer}),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
		}, {
			// Finalizer should be removed.
			Object: ingressWithFinalizers("reconciling-ingress", 1234, ingressTLS, []string{}, &deletionTime),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), networking.KnativeIngressGateway),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "delete IngressTLS",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-ingress", 1234, []v1alpha1.IngressTLS{}, []string{ingressFinalizer}, nil),
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer}),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer}),

			resources.MakeMeshVirtualService(insertProbe(ingressWithFinalizers("reconciling-ingress", 1234, []v1alpha1.IngressTLS{}, []string{ingressFinalizer}, nil)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithFinalizers("reconciling-ingress", 1234, []v1alpha1.IngressTLS{}, []string{ingressFinalizer}, nil)),
				makeGatewayMap([]string{"knative-testing/" + networking.KnativeIngressGateway}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// IngressTLS related TLS servers should be removed from Gateway.
			Object: gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithFinalizersAndStatus("reconciling-ingress", 1234,
				[]string{ingressFinalizer},
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), networking.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "TLS Secret is not in the namespace of Istio gateway service",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
			// No Gateway servers match the given TLS of Ingress.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			// The namespace (`knative-serving`) of the origin secret is different
			// from the namespace (`istio-system`) of Istio gateway service.
			originSecret("knative-serving", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))),
				makeGatewayMap([]string{"knative-testing/" + networking.KnativeIngressGateway}, nil)),

			// The secret copy under istio-system.
			secret("istio-system", targetSecretName, map[string]string{
				networking.OriginSecretNameLabelKey:      "secret0",
				networking.OriginSecretNamespaceLabelKey: "knative-serving",
			}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// ingressTLSServer with the name of the secret copy needs to be added into Gateway.
			Object: gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), networking.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "Reconcile Target secret",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),

			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), irrelevantServer}),
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
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
					)},
				},
				Data: map[string][]byte{
					"wrong_data": []byte("wrongdata"),
				},
			},
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), irrelevantServer}),
			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))),
				makeGatewayMap([]string{"knative-testing/" + networking.KnativeIngressGateway}, nil)),
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
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
					)},
				},
				// The data is expected to be updated to the right one.
				Data: map[string][]byte{
					"test-secret": []byte("abcd"),
				},
			},
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "Reconcile with autoTLS but cluster local visibilty, mesh only",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLSClusterLocal("reconciling-ingress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of Ingress.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(networking.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			resources.MakeMeshVirtualService(insertProbe(ingressWithTLSClusterLocal("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatusClusterLocal("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{{MeshOnly: true}},
					},
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
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
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for Ingress %q", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {

		// As we use a customized resource name for Gateway CRD (i.e. `gateways`), not the one
		// originally generated by kubernetes code generator (i.e. `gatewaies`), we have to
		// explicitly create gateways when setting up the test per suggestion
		// https://github.com/knative/serving/blob/a6852fc3b6cdce72b99c5d578dd64f2e03dabb8b/vendor/k8s.io/client-go/testing/fixture.go#L292
		gateways := getGatewaysFromObjects(listers.GetIstioObjects())
		for _, gateway := range gateways {
			fakeistioclient.Get(ctx).NetworkingV1alpha3().Gateways(gateway.Namespace).Create(gateway)
		}

		return &Reconciler{
			Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			secretLister:         listers.GetSecretLister(),
			tracker:              &NullTracker{},
			finalizer:            ingressFinalizer,
			// Enable reconciling gateway.
			configStore: &testConfigStore{
				config: &config.Config{
					Istio: &config.Istio{
						IngressGateways: []config.Gateway{{
							Namespace:  system.Namespace(),
							Name:       networking.KnativeIngressGateway,
							ServiceURL: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system"),
						}},
					},
					Network: &network.Config{
						HTTPProtocol: network.HTTPDisabled,
					},
				},
			},
			statusManager: &fakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			},
			ingressLister: listers.GetIngressLister(),
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

func gateway(name, namespace string, servers []*istiov1alpha3.Server) *v1alpha3.Gateway {
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: istiov1alpha3.Gateway{
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
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconciling-ingress", 1234))},
		},
		Data: map[string][]byte{
			"test-secret": []byte("abcd"),
		},
	}
}

func withCredentialName(tlsServer *istiov1alpha3.Server, credentialName string) *istiov1alpha3.Server {
	tlsServer.Tls.CredentialName = credentialName
	return tlsServer
}

// Open-coded deepCopy since istio.io/api's Server doesn't provide one currently
func deepCopy(server *istiov1alpha3.Server) *istiov1alpha3.Server {
	return proto.Clone(server).(*istiov1alpha3.Server)
}

func ingressTLSWithSecretNamespace(namespace string) []v1alpha1.IngressTLS {
	result := []v1alpha1.IngressTLS{}
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
	patch := fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":"v1"}}`, finalizer)
	action.Patch = []byte(patch)
	return action
}

func addAnnotations(ing *v1alpha1.Ingress, annos map[string]string) *v1alpha1.Ingress {
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
				ServiceURL: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system"),
			}, {
				Namespace:  system.Namespace(),
				Name:       networking.KnativeIngressGateway,
				ServiceURL: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system"),
			}},
		},
		Network: &network.Config{
			AutoTLS: false,
		},
	}
}

func ingressWithStatus(name string, generation int64, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.IngressSpec{
			DeprecatedGeneration: generation,
			Rules:                ingressRules,
			// Deprecated, needed because of DeepCopy behavior
			Visibility: v1alpha1.IngressVisibilityExternalIP,
		},
		Status: status,
	}
}

func ing(name string, generation int64) *v1alpha1.Ingress {
	return ingressWithStatus(name, generation, v1alpha1.IngressStatus{})
}

func ingressWithFinalizers(name string, generation int64, tls []v1alpha1.IngressTLS, finalizers []string, deletionTime *metav1.Time) *v1alpha1.Ingress {
	ingress := ingressWithTLS(name, generation, tls)
	ingress.ObjectMeta.Finalizers = finalizers
	if deletionTime != nil {
		ingress.ObjectMeta.DeletionTimestamp = deletionTime
	}
	return ingress
}

func ingressWithFinalizersAndStatus(name string, generation int64, finalizers []string, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ingress := ingressWithFinalizers(name, generation, []v1alpha1.IngressTLS{}, finalizers, nil)
	ingress.Status = status
	return ingress
}

func ingressWithTLS(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
	return ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{})
}

func ingressWithTLSClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
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

func ingressWithTLSAndStatus(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithStatus(name, generation, status)
	ci.Spec.TLS = tls
	return ci
}

func ingressWithTLSAndStatusClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithTLSClusterLocal(name, generation, tls)
	ci.Status = status
	return ci
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	context.Context,
	context.CancelFunc,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}
	controller := NewController(ctx, configMapWatcher)

	controller.Reconciler.(*Reconciler).statusManager = &fakeStatusManager{
		FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
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

	return ctx, cancel, informers, controller, configMapWatcher
}

func TestGlobalResyncOnUpdateGatewayConfigMap(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestSetup(t)

	grp := errgroup.Group{}

	servingClient := fakeservingclient.Get(ctx)

	h := NewHooks()

	// Check for Ingress created as a signal that syncHandler ran
	h.OnUpdate(&servingClient.Fake, "ingresses", func(obj runtime.Object) HookResult {
		ci := obj.(*v1alpha1.Ingress)
		t.Logf("ingress updated: %q", ci.Name)

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

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatalf("failed to start ingress manager: %v", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	ingress := ingressWithStatus("config-update", 1234,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: ""},
				},
			},
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
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
	ingressClient := servingClient.NetworkingV1alpha1().Ingresses("test-ns")

	// Create a ingress.
	ingressClient.Create(ingress)

	// Test changes in gateway config map. Ingress should get updated appropriately.
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

func insertProbe(ing *v1alpha1.Ingress) *v1alpha1.Ingress {
	ing = ing.DeepCopy()
	ingress.InsertProbe(ing)
	return ing
}

func TestGlobalResyncOnUpdateNetwork(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestSetup(t)

	grp := errgroup.Group{}

	istioClient := fakeistioclient.Get(ctx)

	h := NewHooks()

	// Check for Gateway created as a signal that syncHandler ran
	h.OnUpdate(&istioClient.Fake, "gateways", func(obj runtime.Object) HookResult {
		updatedGateway := obj.(*v1alpha3.Gateway)
		// The expected gateway should include the Istio TLS server.
		expectedGateway := gateway("knative-test-gateway", system.Namespace(), []*istiov1alpha3.Server{ingressTLSServer})
		expectedGateway.Spec.Servers = append(expectedGateway.Spec.Servers, ingressHTTPRedirectServer)
		expectedGateway.Spec.Servers = resources.SortServers(expectedGateway.Spec.Servers)

		if diff := cmp.Diff(updatedGateway, expectedGateway); diff != "" {
			t.Logf("Unexpected Gateway (-want, +got): %v", diff)
			return HookIncomplete
		}

		return HookComplete
	})

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatalf("Failed to start ingress manager: %v", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Errorf("Wait() = %v", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatalf("Failed to start watcher: %v", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	ingress := ingressWithTLSAndStatus("reconciling-ingress", 1234,
		ingressTLS,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: originDomainInternal},
				},
			},
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
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

	ingressClient := fakeservingclient.Get(ctx).NetworkingV1alpha1().Ingresses("test-ns")

	// Create a ingress.
	ingressClient.Create(ingress)

	gatewayClient := istioClient.NetworkingV1alpha3().Gateways(system.Namespace())
	// Create a Gateway
	gatewayClient.Create(gateway("knative-test-gateway", system.Namespace(), []*istiov1alpha3.Server{}))

	// Create origin secret. "ns" namespace is the namespace of ingress gateway service.
	secretClient := fakekubeclient.Get(ctx).CoreV1().Secrets("istio-system")
	secretClient.Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret0",
			Namespace: "istio-system",
			UID:       "123",
		},
	})

	// Test changes in autoTLS of config-network ConfigMap. Ingress should get updated appropriately.
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

type fakeStatusManager struct {
	FakeIsReady func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error)
}

func (m *fakeStatusManager) IsReady(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
	return m.FakeIsReady(ctx, ing)
}
