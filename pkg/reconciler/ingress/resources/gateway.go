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
	"sort"
	"strings"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
)

var httpServerPortName = "http-server"

// Istio Gateway requires to have at least one server. This placeholderServer is used when
// all of the real servers are deleted.
var placeholderServer = v1alpha3.Server{
	Hosts: []string{"place-holder.place-holder"},
	Port: v1alpha3.Port{
		Name:     "place-holder",
		Number:   9999,
		Protocol: v1alpha3.ProtocolHTTP,
	},
}

// GetServers gets the `Servers` from `Gateway` that belongs to the given ClusterIngress.
func GetServers(gateway *v1alpha3.Gateway, ci *v1alpha1.ClusterIngress) []v1alpha3.Server {
	servers := []v1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		if belongsToClusterIngress(&gateway.Spec.Servers[i], ci) {
			servers = append(servers, gateway.Spec.Servers[i])
		}
	}
	return SortServers(servers)
}

// GetHTTPServer gets the HTTP `Server` from `Gateway`.
func GetHTTPServer(gateway *v1alpha3.Gateway) *v1alpha3.Server {
	for _, server := range gateway.Spec.Servers {
		if server.Port.Name == httpServerPortName {
			return &server
		}
	}
	return nil
}

func belongsToClusterIngress(server *v1alpha3.Server, ci *v1alpha1.ClusterIngress) bool {
	// The format of the portName should be "<clusteringress-name>:<number>".
	// For example, route-test:0.
	portNameSplits := strings.Split(server.Port.Name, ":")
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == ci.Name
}

// SortServers sorts `Server` according to its port name.
func SortServers(servers []v1alpha3.Server) []v1alpha3.Server {
	sort.Slice(servers, func(i, j int) bool {
		return strings.Compare(servers[i].Port.Name, servers[j].Port.Name) < 0
	})
	return servers
}

// MakeServers creates the expected Gateway `Servers` based on the given
// ClusterIngress.
func MakeServers(ci *v1alpha1.ClusterIngress, gatewayServiceNamespace string, originSecrets map[string]*corev1.Secret) ([]v1alpha3.Server, error) {
	servers := []v1alpha3.Server{}
	// TODO(zhiminx): for the hosts that does not included in the ClusterIngressTLS but listed in the ClusterIngressRule,
	// do we consider them as hosts for HTTP?
	for i, tls := range ci.Spec.TLS {
		credentialName := tls.SecretName
		// If the origin secret is not in the target namespace, then it should have been
		// copied into the target namespace. So we use the name of the copy.
		if tls.SecretNamespace != gatewayServiceNamespace {
			originSecret, ok := originSecrets[secretKey(tls)]
			if !ok {
				return nil, fmt.Errorf("unable to get the original secret %s/%s", tls.SecretNamespace, tls.SecretName)
			}
			credentialName = targetSecret(originSecret, ci)
		}
		servers = append(servers, v1alpha3.Server{
			Hosts: tls.Hosts,
			Port: v1alpha3.Port{
				Name:     fmt.Sprintf("%s:%d", ci.Name, i),
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: tls.ServerCertificate,
				PrivateKey:        tls.PrivateKey,
				CredentialName:    credentialName,
			},
		})
	}
	return SortServers(servers), nil
}

// MakeHTTPServer creates a HTTP Gateway `Server` based on the HTTPProtocol
// configureation.
func MakeHTTPServer(httpProtocol network.HTTPProtocol) *v1alpha3.Server {
	if httpProtocol == network.HTTPDisabled {
		return nil
	}
	server := &v1alpha3.Server{
		Hosts: []string{"*"},
		Port: v1alpha3.Port{
			Name:     httpServerPortName,
			Number:   80,
			Protocol: v1alpha3.ProtocolHTTP,
		},
	}
	if httpProtocol == network.HTTPRedirected {
		server.TLS = &v1alpha3.TLSOptions{
			HTTPSRedirect: true,
		}
	}
	return server
}

// GatewayServiceNamespace returns the namespace of the gateway service that the `Gateway` object
// with name `gatewayName` is associated with.
func GatewayServiceNamespace(ingressGateways []config.Gateway, gatewayName string) (string, error) {
	for _, gw := range ingressGateways {
		if gw.GatewayName != gatewayName {
			continue
		}
		// serviceURL should be of the form serviceName.namespace.<domain>, for example
		// serviceName.namespace.svc.cluster.local.
		parts := strings.SplitN(gw.ServiceURL, ".", 3)
		if len(parts) != 3 {
			return "", fmt.Errorf("unexpected service URL form: %s", gw.ServiceURL)
		}
		return parts[1], nil
	}
	return "", fmt.Errorf("no Gateway configuration is found for gateway %s", gatewayName)
}

// getAllGatewaySvcNamespaces gets all of the namespaces of Istio gateway services from context.
func getAllGatewaySvcNamespaces(ctx context.Context) []string {
	cfg := config.FromContext(ctx).Istio
	namespaces := sets.String{}
	for _, ingressgateway := range cfg.IngressGateways {
		// serviceURL should be of the form serviceName.namespace.<domain>, for example
		// serviceName.namespace.svc.cluster.local.

		ns := strings.Split(ingressgateway.ServiceURL, ".")[1]
		namespaces.Insert(ns)
	}
	return namespaces.List()
}

// UpdateGateway replaces the existing servers with the wanted servers.
func UpdateGateway(gateway *v1alpha3.Gateway, want []v1alpha3.Server, existing []v1alpha3.Server) *v1alpha3.Gateway {
	existingServers := sets.String{}
	for i := range existing {
		existingServers.Insert(existing[i].Port.Name)
	}

	servers := []v1alpha3.Server{}
	for _, server := range gateway.Spec.Servers {
		// We remove
		//  1) the existing servers
		//  2) the default HTTP server and HTTPS server in the gateway because they are only used for the scenario of not reconciling gateway.
		//  3) the placeholder servers.
		if existingServers.Has(server.Port.Name) || isDefaultServer(&server) || isPlaceHolderServer(&server) {
			continue
		}
		servers = append(servers, server)
	}
	servers = append(servers, want...)

	// Istio Gateway requires to have at least one server. So if the final gateway does not have any server,
	// we add "placeholder" server back.
	if len(servers) == 0 {
		servers = append(servers, placeholderServer)
	}

	SortServers(servers)
	gateway.Spec.Servers = servers
	return gateway
}

func isDefaultServer(server *v1alpha3.Server) bool {
	return server.Port.Name == "http" || server.Port.Name == "https"
}

func isPlaceHolderServer(server *v1alpha3.Server) bool {
	return equality.Semantic.DeepEqual(server, &placeholderServer)
}
