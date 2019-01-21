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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
var clusterIngress = v1alpha1.ClusterIngress{
	ObjectMeta: metav1.ObjectMeta{
		Name: "clusteringress",
	},
	Spec: v1alpha1.IngressSpec{
		TLS: []v1alpha1.ClusterIngressTLS{{
			Hosts:             []string{"host1.example.com"},
			SecretName:        "secret0",
			SecretNamespace:   "istio-system",
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

func TestMakeServers(t *testing.T) {
	servers := MakeServers(&clusterIngress)
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

func TestUpdateGateway(t *testing.T) {
	want := []v1alpha3.Server{{
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
	}}

	existing := []v1alpha3.Server{{
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

	result := UpdateGateway(gateway.DeepCopy(), want, existing)
	expected := &v1alpha3.Gateway{
		Spec: v1alpha3.GatewaySpec{
			Servers: []v1alpha3.Server{{
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
	}
	if diff := cmp.Diff(expected, result); diff != "" {
		t.Errorf("Unexpected gateway (-want +got): %v", diff)
	}
}
