/*
Copyright 2019 The Knative Authors.

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

package resources

import (
	"context"
	"fmt"
	"hash/adler32"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/kmeta"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/ingress/config"
)

var secret = corev1.Secret{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "secret0",
		Namespace: system.Namespace(),
	},
	Data: map[string][]byte{
		"test": []byte("test"),
	},
}

var originSecrets = map[string]*corev1.Secret{
	fmt.Sprintf("%s/secret0", system.Namespace()): &secret,
}

var selector = map[string]string{
	"istio": "ingressgateway",
}

var gateway = v1alpha3.Gateway{
	Spec: istiov1alpha3.Gateway{
		Servers: servers,
	},
}

var servers = []*istiov1alpha3.Server{{
	Hosts: []string{"host1.example.com"},
	Port: &istiov1alpha3.Port{
		Name:     "test-ns/ingress:0",
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1alpha3.Server_TLSOptions{
		Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}, {
	Hosts: []string{"host2.example.com"},
	Port: &istiov1alpha3.Port{
		Name:     "test-ns/non-ingress:0",
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1alpha3.Server_TLSOptions{
		Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}}

var httpServer = istiov1alpha3.Server{
	Hosts: []string{"*"},
	Port: &istiov1alpha3.Port{
		Name:     httpServerPortName,
		Number:   80,
		Protocol: "HTTP",
	},
}

var gatewayWithPlaceholderServer = v1alpha3.Gateway{
	Spec: istiov1alpha3.Gateway{
		Servers: []*istiov1alpha3.Server{&placeholderServer},
	},
}

var gatewayWithDefaultWildcardTLSServer = v1alpha3.Gateway{
	Spec: istiov1alpha3.Gateway{
		Servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode: istiov1alpha3.Server_TLSOptions_SIMPLE,
			}},
		},
	},
}

var gatewayWithModifiedWildcardTLSServer = v1alpha3.Gateway{
	Spec: istiov1alpha3.Gateway{
		Servers: []*istiov1alpha3.Server{&modifiedDefaultTLSServer},
	},
}

var modifiedDefaultTLSServer = istiov1alpha3.Server{
	Hosts: []string{"added.by.user.example.com"},
	Port: &istiov1alpha3.Port{
		Name:     "https",
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1alpha3.Server_TLSOptions{
		Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}

var ingressSpec = v1alpha1.IngressSpec{
	Rules: []v1alpha1.IngressRule{{
		Hosts: []string{"host1.example.com"},
	}},
	TLS: []v1alpha1.IngressTLS{{
		Hosts:           []string{"host1.example.com"},
		SecretName:      "secret0",
		SecretNamespace: system.Namespace(),
	}},
}

var ingressResource = v1alpha1.Ingress{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ingress",
		Namespace: "test-ns",
	},
	Spec: ingressSpec,
}

func TestGetServers(t *testing.T) {
	servers := GetServers(&gateway, &ingressResource)
	expected := []*istiov1alpha3.Server{{
		Hosts: []string{"host1.example.com"},
		Port: &istiov1alpha3.Port{
			Name:     "test-ns/ingress:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.Server_TLSOptions{
			Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
			ServerCertificate: corev1.TLSCertKey,
			PrivateKey:        corev1.TLSPrivateKeyKey,
		},
	}}

	if diff := cmp.Diff(expected, servers); diff != "" {
		t.Errorf("Unexpected servers (-want +got): %v", diff)
	}
}

func TestGetHTTPServer(t *testing.T) {
	newGateway := gateway
	newGateway.Spec.Servers = append(newGateway.Spec.Servers, &httpServer)
	server := GetHTTPServer(&newGateway)
	expected := &istiov1alpha3.Server{
		Hosts: []string{"*"},
		Port: &istiov1alpha3.Port{
			Name:     httpServerPortName,
			Number:   80,
			Protocol: "HTTP",
		},
	}
	if diff := cmp.Diff(expected, server); diff != "" {
		t.Errorf("Unexpected server (-want +got): %v", diff)
	}
}

func TestMakeTLSServers(t *testing.T) {
	cases := []struct {
		name                    string
		ci                      *v1alpha1.Ingress
		gatewayServiceNamespace string
		originSecrets           map[string]*corev1.Secret
		expected                []*istiov1alpha3.Server
		wantErr                 bool
	}{{
		name: "secret namespace is the different from the gateway service namespace",
		ci:   &ingressResource,
		// gateway service namespace is "istio-system", while the secret namespace is system.Namespace()("knative-testing").
		gatewayServiceNamespace: "istio-system",
		originSecrets:           originSecrets,
		expected: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    targetSecret(&secret, &ingressResource),
			},
		}},
	}, {
		name: "secret namespace is the same as the gateway service namespace",
		ci:   &ingressResource,
		// gateway service namespace and the secret namespace are both in system.Namespace().
		gatewayServiceNamespace: system.Namespace(),
		originSecrets:           originSecrets,
		expected: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    "secret0",
			},
		}},
	}, {
		name:                    "port name is created with ingress namespace-name",
		ci:                      &ingressResource,
		gatewayServiceNamespace: system.Namespace(),
		originSecrets:           originSecrets,
		expected: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				// port name is created with <namespace>/<name>
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    "secret0",
			},
		}},
	}, {
		name:                    "error to make servers because of incorrect originSecrets",
		ci:                      &ingressResource,
		gatewayServiceNamespace: "istio-system",
		originSecrets:           map[string]*corev1.Secret{},
		wantErr:                 true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			servers, err := MakeTLSServers(c.ci, c.gatewayServiceNamespace, c.originSecrets)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeServers error = %v, WantErr %v", c.name, err, c.wantErr)
			}
			if diff := cmp.Diff(c.expected, servers); diff != "" {
				t.Errorf("Unexpected servers (-want, +got): %v", diff)
			}
		})
	}
}

func TestMakeHTTPServer(t *testing.T) {
	cases := []struct {
		name         string
		httpProtocol network.HTTPProtocol
		expected     *istiov1alpha3.Server
	}{{
		name:         "nil HTTP Server",
		httpProtocol: network.HTTPDisabled,
		expected:     nil,
	}, {
		name:         "HTTP server",
		httpProtocol: network.HTTPEnabled,
		expected: &istiov1alpha3.Server{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     httpServerPortName,
				Number:   80,
				Protocol: "HTTP",
			},
		},
	}, {
		name:         "Redirect HTTP server",
		httpProtocol: network.HTTPRedirected,
		expected: &istiov1alpha3.Server{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     httpServerPortName,
				Number:   80,
				Protocol: "HTTP",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				HttpsRedirect: true,
			},
		},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MakeHTTPServer(c.httpProtocol, []string{"*"})
			if diff := cmp.Diff(c.expected, got); diff != "" {
				t.Errorf("Unexpected HTTP Server (-want, +got): %v", diff)
			}
		})
	}
}

func TestUpdateGateway(t *testing.T) {
	cases := []struct {
		name            string
		existingServers []*istiov1alpha3.Server
		newServers      []*istiov1alpha3.Server
		original        v1alpha3.Gateway
		expected        v1alpha3.Gateway
	}{{
		name: "Update Gateway servers.",
		existingServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		newServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host-new.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		original: gateway,
		expected: v1alpha3.Gateway{
			Spec: istiov1alpha3.Gateway{
				Servers: []*istiov1alpha3.Server{{
					// The host name was updated to the one in "newServers".
					Hosts: []string{"host-new.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}, {
					Hosts: []string{"host2.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/non-ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway",
		existingServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		newServers: []*istiov1alpha3.Server{},
		original:   gateway,
		expected: v1alpha3.Gateway{
			Spec: istiov1alpha3.Gateway{
				// Only one server is left. The other one is deleted.
				Servers: []*istiov1alpha3.Server{{
					Hosts: []string{"host2.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/non-ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway and no real servers are left",

		// All of the servers in the original gateway will be deleted.
		existingServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}, {
			Hosts: []string{"host2.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/non-ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		newServers: []*istiov1alpha3.Server{},
		original:   gateway,
		expected:   gatewayWithPlaceholderServer,
	}, {
		name:            "Add servers to the gateway with only placeholder server",
		existingServers: []*istiov1alpha3.Server{},
		newServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		original: gatewayWithPlaceholderServer,
		// The placeholder server should be deleted.
		expected: v1alpha3.Gateway{
			Spec: istiov1alpha3.Gateway{
				Servers: []*istiov1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}},
			},
		},
	}, {
		name:            "Delete wildcard servers from gateway",
		existingServers: []*istiov1alpha3.Server{},
		newServers:      servers,
		original:        gatewayWithDefaultWildcardTLSServer,
		// The wildcard server should be deleted.
		expected: gateway,
	}, {
		name:            "Do not delete modified wildcard servers from gateway",
		existingServers: []*istiov1alpha3.Server{},
		newServers: []*istiov1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		original: gatewayWithModifiedWildcardTLSServer,
		expected: v1alpha3.Gateway{
			Spec: istiov1alpha3.Gateway{
				Servers: []*istiov1alpha3.Server{
					{
						Hosts: []string{"host1.example.com"},
						Port: &istiov1alpha3.Port{
							Name:     "clusteringress:0",
							Number:   443,
							Protocol: "HTTPS",
						},
						Tls: &istiov1alpha3.Server_TLSOptions{
							Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
							ServerCertificate: corev1.TLSCertKey,
							PrivateKey:        corev1.TLSPrivateKeyKey,
						},
					},
					&modifiedDefaultTLSServer,
				},
			},
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := UpdateGateway(&c.original, c.newServers, c.existingServers)
			if diff := cmp.Diff(&c.expected, g); diff != "" {
				t.Errorf("Unexpected gateway (-want, +got): %v", diff)
			}
		})
	}
}

func TestMakeIngressGateways(t *testing.T) {
	cases := []struct {
		name           string
		ia             *v1alpha1.Ingress
		originSecrets  map[string]*corev1.Secret
		gatewayService *corev1.Service
		want           []*v1alpha3.Gateway
		wantErr        bool
	}{{
		name:          "happy path: secret namespace is the different from the gateway service namespace",
		ia:            &ingressResource,
		originSecrets: originSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1alpha3.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/istio-ingressgateway"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1alpha3.Gateway{
				Selector: selector,
				Servers: []*istiov1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
						CredentialName:    targetSecret(&secret, &ingressResource),
					},
				}, {
					Hosts: []string{"host1.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     httpServerPortName,
						Number:   80,
						Protocol: "HTTP",
					},
				}},
			},
		}},
	}, {
		name:          "happy path: secret namespace is the same as the gateway service namespace",
		ia:            &ingressResource,
		originSecrets: originSecrets,
		// The namespace of gateway service is the same as the secrets.
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: system.Namespace(),
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1alpha3.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte(system.Namespace()+"/istio-ingressgateway"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1alpha3.Gateway{
				Selector: selector,
				Servers: []*istiov1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     "test-ns/ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1alpha3.Server_TLSOptions{
						Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
						CredentialName:    secret.Name,
					},
				}, {
					Hosts: []string{"host1.example.com"},
					Port: &istiov1alpha3.Port{
						Name:     httpServerPortName,
						Number:   80,
						Protocol: "HTTP",
					},
				}},
			},
		}},
	}, {
		name:          "error to make gateway because of incorrect originSecrets",
		ia:            &ingressResource,
		originSecrets: map[string]*corev1.Secret{},
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		wantErr: true,
	}}

	for _, c := range cases {
		ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
		defer cancel()
		svcLister := serviceLister(ctx, c.gatewayService)
		ctx = config.ToContext(context.Background(), &config.Config{
			Istio: &config.Istio{
				IngressGateways: []config.Gateway{{
					Name:       networking.KnativeIngressGateway,
					ServiceURL: fmt.Sprintf("%s.%s.svc.cluster.local", c.gatewayService.Name, c.gatewayService.Namespace),
				}},
			},
			Network: &network.Config{
				HTTPProtocol: network.HTTPEnabled,
			},
		})
		t.Run(c.name, func(t *testing.T) {
			got, err := MakeIngressGateways(ctx, c.ia, c.originSecrets, svcLister)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeIngressGateways error = %v, WantErr %v", c.name, err, c.wantErr)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("Unexpected Gateways (-want, +got): %v", diff)
			}
		})
	}
}

func serviceLister(ctx context.Context, svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := fakekubeclient.Get(ctx)
	informer := fakeserviceinformer.Get(ctx)

	for _, svc := range svcs {
		fake.CoreV1().Services(svc.Namespace).Create(svc)
		informer.Informer().GetIndexer().Add(svc)
	}

	return informer.Lister()
}

func TestGatewayName(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway",
			Namespace: "istio-system",
		},
	}
	ingress := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress",
			Namespace: "default",
		},
	}

	want := fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/gateway")))
	got := GatewayName(ingress, svc)
	if got != want {
		t.Errorf("Unexpected gateway name. want %q, got %q", want, got)
	}
}
