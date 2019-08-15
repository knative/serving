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

	"knative.dev/serving/pkg/apis/networking"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
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
	Spec: v1alpha3.GatewaySpec{
		Servers: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}, {
			Hosts: []string{"host2.example.com"},
			Port: v1alpha3.Port{
				Name:     "non-clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
	},
}

var httpServer = v1alpha3.Server{
	Hosts: []string{"*"},
	Port: v1alpha3.Port{
		Name:     httpServerPortName,
		Number:   80,
		Protocol: v1alpha3.ProtocolHTTP,
	},
}

var gatewayWithPlaceholderServer = v1alpha3.Gateway{
	Spec: v1alpha3.GatewaySpec{
		Servers: []v1alpha3.Server{placeholderServer},
	},
}

var clusterIngress = v1alpha1.ClusterIngress{
	ObjectMeta: metav1.ObjectMeta{
		Name: "clusteringress",
	},
	Spec: v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts: []string{"host1.example.com"},
		}},
		TLS: []v1alpha1.IngressTLS{{
			Hosts:             []string{"host1.example.com"},
			SecretName:        "secret0",
			SecretNamespace:   system.Namespace(),
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
		}},
	},
}

func TestGetServers(t *testing.T) {
	servers := GetServers(&gateway, &clusterIngress)
	expected := []v1alpha3.Server{{
		Hosts: []string{"host1.example.com"},
		Port: v1alpha3.Port{
			Name:     "clusteringress:0",
			Number:   443,
			Protocol: v1alpha3.ProtocolHTTPS,
		},
		TLS: &v1alpha3.TLSOptions{
			Mode:              v1alpha3.TLSModeSimple,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
		},
	}}

	if diff := cmp.Diff(expected, servers); diff != "" {
		t.Errorf("Unexpected servers (-want +got): %v", diff)
	}
}

func TestGetHTTPServer(t *testing.T) {
	newGateway := gateway
	newGateway.Spec.Servers = append(newGateway.Spec.Servers, httpServer)
	server := GetHTTPServer(&newGateway)
	expected := v1alpha3.Server{
		Hosts: []string{"*"},
		Port: v1alpha3.Port{
			Name:     httpServerPortName,
			Number:   80,
			Protocol: v1alpha3.ProtocolHTTP,
		},
	}
	if diff := cmp.Diff(expected, *server); diff != "" {
		t.Errorf("Unexpected server (-want +got): %v", diff)
	}
}

func TestMakeTLSServers(t *testing.T) {
	cases := []struct {
		name                    string
		ci                      *v1alpha1.ClusterIngress
		gatewayServiceNamespace string
		originSecrets           map[string]*corev1.Secret
		expected                []v1alpha3.Server
		wantErr                 bool
	}{{
		name: "secret namespace is the different from the gateway service namespace",
		ci:   &clusterIngress,
		// gateway service namespace is "istio-system", while the secret namespace is system.Namespace()("knative-testing").
		gatewayServiceNamespace: "istio-system",
		originSecrets:           originSecrets,
		expected: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
				CredentialName:    targetSecret(&secret, &clusterIngress),
			},
		}},
	}, {
		name: "secret namespace is the same as the gateway service namespace",
		ci:   &clusterIngress,
		// gateway service namespace and the secret namespace are both in system.Namespace().
		gatewayServiceNamespace: system.Namespace(),
		originSecrets:           originSecrets,
		expected: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
				CredentialName:    "secret0",
			},
		}},
	}, {
		name:                    "error to make servers because of incorrect originSecrets",
		ci:                      &clusterIngress,
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
		expected     *v1alpha3.Server
	}{{
		name:         "nil HTTP Server",
		httpProtocol: network.HTTPDisabled,
		expected:     nil,
	}, {
		name:         "HTTP server",
		httpProtocol: network.HTTPEnabled,
		expected: &v1alpha3.Server{
			Hosts: []string{"*"},
			Port: v1alpha3.Port{
				Name:     httpServerPortName,
				Number:   80,
				Protocol: "HTTP",
			},
		},
	}, {
		name:         "Redirect HTTP server",
		httpProtocol: network.HTTPRedirected,
		expected: &v1alpha3.Server{
			Hosts: []string{"*"},
			Port: v1alpha3.Port{
				Name:     httpServerPortName,
				Number:   80,
				Protocol: "HTTP",
			},
			TLS: &v1alpha3.TLSOptions{
				HTTPSRedirect: true,
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
		existingServers []v1alpha3.Server
		newServers      []v1alpha3.Server
		original        v1alpha3.Gateway
		expected        v1alpha3.Gateway
	}{{
		name: "Update Gateway servers.",
		existingServers: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
		newServers: []v1alpha3.Server{{
			Hosts: []string{"host-new.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
		original: gateway,
		expected: v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					// The host name was updated to the one in "newServers".
					Hosts: []string{"host-new.example.com"},
					Port: v1alpha3.Port{
						Name:     "clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
					},
				}, {
					Hosts: []string{"host2.example.com"},
					Port: v1alpha3.Port{
						Name:     "non-clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway",
		existingServers: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
		newServers: []v1alpha3.Server{},
		original:   gateway,
		expected: v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				// Only one server is left. The other one is deleted.
				Servers: []v1alpha3.Server{{
					Hosts: []string{"host2.example.com"},
					Port: v1alpha3.Port{
						Name:     "non-clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway and no real servers are left",

		// All of the servers in the original gateway will be deleted.
		existingServers: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}, {
			Hosts: []string{"host2.example.com"},
			Port: v1alpha3.Port{
				Name:     "non-clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
		newServers: []v1alpha3.Server{},
		original:   gateway,
		expected:   gatewayWithPlaceholderServer,
	}, {
		name:            "Add servers to the gateway with only placeholder server",
		existingServers: []v1alpha3.Server{},
		newServers: []v1alpha3.Server{{
			Hosts: []string{"host1.example.com"},
			Port: v1alpha3.Port{
				Name:     "clusteringress:0",
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: "tls.crt",
				PrivateKey:        "tls.key",
			},
		}},
		original: gatewayWithPlaceholderServer,
		// The placeholder server should be deleted.
		expected: v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: v1alpha3.Port{
						Name:     "clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
					},
				}},
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
		ia             v1alpha1.IngressAccessor
		originSecrets  map[string]*corev1.Secret
		gatewayService *corev1.Service
		want           []*v1alpha3.Gateway
		wantErr        bool
	}{{
		name:          "happy path: secret namespace is the different from the gateway service namespace",
		ia:            &clusterIngress,
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
				Name:            fmt.Sprintf("clusteringress-%d", adler32.Checksum([]byte("istio-system/istio-ingressgateway"))),
				Namespace:       system.Namespace(),
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&clusterIngress)},
				Labels: map[string]string{
					networking.IngressLabelKey: "clusteringress",
				},
			},
			Spec: v1alpha3.GatewaySpec{
				Selector: selector,
				Servers: []v1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: v1alpha3.Port{
						Name:     "clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
						CredentialName:    targetSecret(&secret, &clusterIngress),
					},
				}, {
					Hosts: []string{"host1.example.com"},
					Port: v1alpha3.Port{
						Name:     httpServerPortName,
						Number:   80,
						Protocol: "HTTP",
					},
				}},
			},
		}},
	}, {
		name:          "happy path: secret namespace is the same as the gateway service namespace",
		ia:            &clusterIngress,
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
				Name:            fmt.Sprintf("clusteringress-%d", adler32.Checksum([]byte(system.Namespace()+"/istio-ingressgateway"))),
				Namespace:       system.Namespace(),
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&clusterIngress)},
				Labels: map[string]string{
					networking.IngressLabelKey: "clusteringress",
				},
			},
			Spec: v1alpha3.GatewaySpec{
				Selector: selector,
				Servers: []v1alpha3.Server{{
					Hosts: []string{"host1.example.com"},
					Port: v1alpha3.Port{
						Name:     "clusteringress:0",
						Number:   443,
						Protocol: v1alpha3.ProtocolHTTPS,
					},
					TLS: &v1alpha3.TLSOptions{
						Mode:              v1alpha3.TLSModeSimple,
						ServerCertificate: "tls.crt",
						PrivateKey:        "tls.key",
						CredentialName:    secret.Name,
					},
				}, {
					Hosts: []string{"host1.example.com"},
					Port: v1alpha3.Port{
						Name:     httpServerPortName,
						Number:   80,
						Protocol: "HTTP",
					},
				}},
			},
		}},
	}, {
		name:          "error to make gateway because of incorrect originSecrets",
		ia:            &clusterIngress,
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
		svcLister := serviceLister(c.gatewayService)
		ctx := config.ToContext(context.Background(), &config.Config{
			Istio: &config.Istio{
				IngressGateways: []config.Gateway{{
					Name:       "knative-ingress-gateway",
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

func serviceLister(svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	services := informer.Core().V1().Services()

	for _, svc := range svcs {
		fake.Core().Services(svc.Namespace).Create(svc)
		services.Informer().GetIndexer().Add(svc)
	}

	return services.Lister()
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

func TestCanProbeGateway(t *testing.T) {
	cases := []struct {
		name     string
		gateway  *v1alpha3.Gateway
		canProbe bool
	}{{
		name: "wildcard HTTP on port 80",
		gateway: &v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"*"},
					Port: v1alpha3.Port{
						Number:   80,
						Protocol: v1alpha3.ProtocolHTTP,
					},
				}},
			},
		},
		canProbe: true,
	}, {
		name: "specific host HTTP on port 80",
		gateway: &v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"foo.bar.com"},
					Port: v1alpha3.Port{
						Number:   100,
						Protocol: v1alpha3.ProtocolHTTP,
					},
				}},
			},
		},
		canProbe: false,
	}, {
		name: "wildcard HTTP on port !80",
		gateway: &v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"*"},
					Port: v1alpha3.Port{
						Number:   100,
						Protocol: v1alpha3.ProtocolHTTP,
					},
				}},
			},
		},
		canProbe: false,
	}, {
		name: "wildcard TCP on port 80",
		gateway: &v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"*"},
					Port: v1alpha3.Port{
						Number:   80,
						Protocol: v1alpha3.ProtocolTCP,
					},
				}},
			},
		},
		canProbe: false,
	}, {
		name: "wildcard HTTPS on port 443",
		gateway: &v1alpha3.Gateway{
			Spec: v1alpha3.GatewaySpec{
				Servers: []v1alpha3.Server{{
					Hosts: []string{"*"},
					Port: v1alpha3.Port{
						Number:   100,
						Protocol: v1alpha3.ProtocolHTTP,
					},
				}},
			},
		},
		canProbe: false,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CanProbeGateway(c.gateway)
			if got != c.canProbe {
				t.Errorf("Expected %t, got %t", c.canProbe, got)
			}
		})
	}
}
