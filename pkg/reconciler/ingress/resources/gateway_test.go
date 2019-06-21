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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestMakeServers(t *testing.T) {
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
			servers, err := MakeServers(c.ci, c.gatewayServiceNamespace, c.originSecrets)
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
			got := MakeHTTPServer(c.httpProtocol)
			if diff := cmp.Diff(c.expected, got); diff != "" {
				t.Errorf("Unexpected HTTP Server (-want, +got): %v", diff)
			}
		})
	}
}

func TestGatewayServiceNamespace(t *testing.T) {
	cases := []struct {
		name            string
		ingressGateways []config.Gateway
		gatewayName     string
		expected        string
		wantErr         bool
	}{{
		name: "Gateway service exists.",
		ingressGateways: []config.Gateway{{
			GatewayName: "test-gateway",
			ServiceURL:  "istio-ingressgateway.istio-system.svc.cluster.local",
		}},
		gatewayName: "test-gateway",
		expected:    "istio-system",
		wantErr:     false,
	}, {
		name: "Gateway service does not exists.",
		ingressGateways: []config.Gateway{{
			GatewayName: "test-gateway",
			ServiceURL:  "istio-ingressgateway.istio-system.svc.cluster.local",
		}},
		gatewayName: "non-exist-gateway",
		wantErr:     true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gatewayServiceNamespace, err := GatewayServiceNamespace(c.ingressGateways, c.gatewayName)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; GatewayServiceNamespace error = %v, WantErr %v", c.name, err, c.wantErr)
			}

			if diff := cmp.Diff(c.expected, gatewayServiceNamespace); diff != "" {
				t.Errorf("Unexpected gateway service namespace (-want, +got): %v", diff)
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
