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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/ingress/config"
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

// GetServers gets the `Servers` from `Gateway` that belongs to the given Ingress.
func GetServers(gateway *v1alpha3.Gateway, ia v1alpha1.IngressAccessor) []v1alpha3.Server {
	servers := []v1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		if belongsToIngress(&gateway.Spec.Servers[i], ia) {
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

func belongsToIngress(server *v1alpha3.Server, ia v1alpha1.IngressAccessor) bool {
	// The format of the portName should be "<namespace>/<ingress_name>:<number>".
	// For example, default/routetest:0.
	portNameSplits := strings.Split(server.Port.Name, ":")
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == ia.GetNamespace()+"/"+ia.GetName()
}

// SortServers sorts `Server` according to its port name.
func SortServers(servers []v1alpha3.Server) []v1alpha3.Server {
	sort.Slice(servers, func(i, j int) bool {
		return strings.Compare(servers[i].Port.Name, servers[j].Port.Name) < 0
	})
	return servers
}

// MakeIngressGateways creates Gateways for a given IngressAccessor.
func MakeIngressGateways(ctx context.Context, ia v1alpha1.IngressAccessor, originSecrets map[string]*corev1.Secret, svcLister corev1listers.ServiceLister) ([]*v1alpha3.Gateway, error) {
	gatewayServices, err := getGatewayServices(ctx, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1alpha3.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		gateway, err := makeIngressGateway(ctx, ia, originSecrets, gatewayService.Spec.Selector, gatewayService)
		if err != nil {
			return nil, err
		}
		gateways[i] = gateway
	}
	return gateways, nil
}

func makeIngressGateway(ctx context.Context, ia v1alpha1.IngressAccessor, originSecrets map[string]*corev1.Secret, selector map[string]string, gatewayService *corev1.Service) (*v1alpha3.Gateway, error) {
	ns := ia.GetNamespace()
	if len(ns) == 0 {
		ns = system.Namespace()
	}
	servers, err := MakeTLSServers(ia, gatewayService.Namespace, originSecrets)
	if err != nil {
		return nil, err
	}
	hosts := sets.String{}
	for _, rule := range ia.GetSpec().Rules {
		hosts.Insert(rule.Hosts...)
	}
	servers = append(servers, *MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol, hosts.List()))
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:            GatewayName(ia, gatewayService),
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ia)},
			Labels: map[string]string{
				// We need this label to find out all of Gateways of a given Ingress.
				networking.IngressLabelKey: ia.GetName(),
			},
		},
		Spec: v1alpha3.GatewaySpec{
			Selector: selector,
			Servers:  servers,
		},
	}, nil
}

func getGatewayServices(ctx context.Context, svcLister corev1listers.ServiceLister) ([]*corev1.Service, error) {
	ingressSvcMetas, err := getIngressGatewaySvcNameNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	services := make([]*corev1.Service, len(ingressSvcMetas))
	for i, ingressSvcMeta := range ingressSvcMetas {
		svc, err := svcLister.Services(ingressSvcMeta.Namespace).Get(ingressSvcMeta.Name)
		if err != nil {
			return nil, err
		}
		services[i] = svc
	}
	return services, nil
}

// GatewayName create a name for the Gateway that is built based on the given IngressAccessor and bonds to the
// given ingress gateway service.
func GatewayName(accessor kmeta.Accessor, gatewaySvc *corev1.Service) string {
	gatewayServiceKey := fmt.Sprintf("%s/%s", gatewaySvc.Namespace, gatewaySvc.Name)
	return fmt.Sprintf("%s-%d", accessor.GetName(), adler32.Checksum([]byte(gatewayServiceKey)))
}

// MakeTLSServers creates the expected Gateway TLS `Servers` based on the given
// IngressAccessor.
func MakeTLSServers(ia v1alpha1.IngressAccessor, gatewayServiceNamespace string, originSecrets map[string]*corev1.Secret) ([]v1alpha3.Server, error) {
	servers := make([]v1alpha3.Server, len(ia.GetSpec().TLS))
	// TODO(zhiminx): for the hosts that does not included in the IngressTLS but listed in the IngressRule,
	// do we consider them as hosts for HTTP?
	for i, tls := range ia.GetSpec().TLS {
		credentialName := tls.SecretName
		// If the origin secret is not in the target namespace, then it should have been
		// copied into the target namespace. So we use the name of the copy.
		if tls.SecretNamespace != gatewayServiceNamespace {
			originSecret, ok := originSecrets[secretKey(tls)]
			if !ok {
				return nil, fmt.Errorf("unable to get the original secret %s/%s", tls.SecretNamespace, tls.SecretName)
			}
			credentialName = targetSecret(originSecret, ia)
		}

		port := ia.GetNamespace() + "/" + ia.GetName()

		servers[i] = v1alpha3.Server{
			Hosts: tls.Hosts,
			Port: v1alpha3.Port{
				Name:     fmt.Sprintf("%s:%d", port, i),
				Number:   443,
				Protocol: v1alpha3.ProtocolHTTPS,
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: tls.ServerCertificate,
				PrivateKey:        tls.PrivateKey,
				CredentialName:    credentialName,
			},
		}
	}
	return SortServers(servers), nil
}

// MakeHTTPServer creates a HTTP Gateway `Server` based on the HTTPProtocol
// configureation.
func MakeHTTPServer(httpProtocol network.HTTPProtocol, hosts []string) *v1alpha3.Server {
	if httpProtocol == network.HTTPDisabled {
		return nil
	}
	server := &v1alpha3.Server{
		Hosts: hosts,
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

// ServiceNamespaceFromURL extracts the namespace part from the service URL.
// TODO(nghia):  Remove this by parsing at config parsing time.
func ServiceNamespaceFromURL(svc string) (string, error) {
	parts := strings.SplitN(svc, ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("unexpected service URL form: %s", svc)
	}
	return parts[1], nil
}

// TODO(nghia):  Remove this by parsing at config parsing time.
func getIngressGatewaySvcNameNamespaces(ctx context.Context) ([]metav1.ObjectMeta, error) {
	cfg := config.FromContext(ctx).Istio
	nameNamespaces := make([]metav1.ObjectMeta, len(cfg.IngressGateways))
	for i, ingressgateway := range cfg.IngressGateways {
		parts := strings.SplitN(ingressgateway.ServiceURL, ".", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected service URL form: %s", ingressgateway.ServiceURL)
		}
		nameNamespaces[i] = metav1.ObjectMeta{
			Name:      parts[0],
			Namespace: parts[1],
		}
	}
	return nameNamespaces, nil
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
	if server.Port.Name == "https" {
		return len(server.Hosts) > 0 && server.Hosts[0] == "*"
	}
	return server.Port.Name == "http"
}

func isPlaceHolderServer(server *v1alpha3.Server) bool {
	return equality.Semantic.DeepEqual(server, &placeholderServer)
}
