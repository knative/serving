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

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
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
var placeholderServer = istiov1alpha3.Server{
	Hosts: []string{"place-holder.place-holder"},
	Port: &istiov1alpha3.Port{
		Name:     "place-holder",
		Number:   9999,
		Protocol: "HTTP",
	},
}

// GetServers gets the `Servers` from `Gateway` that belongs to the given Ingress.
func GetServers(gateway *v1alpha3.Gateway, ing *v1alpha1.Ingress) []*istiov1alpha3.Server {
	servers := []*istiov1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		if belongsToIngress(gateway.Spec.Servers[i], ing) {
			servers = append(servers, gateway.Spec.Servers[i])
		}
	}
	return SortServers(servers)
}

// GetHTTPServer gets the HTTP `Server` from `Gateway`.
func GetHTTPServer(gateway *v1alpha3.Gateway) *istiov1alpha3.Server {
	for _, server := range gateway.Spec.Servers {
		if server.Port.Name == httpServerPortName {
			return server
		}
	}
	return nil
}

func belongsToIngress(server *istiov1alpha3.Server, ing *v1alpha1.Ingress) bool {
	// The format of the portName should be "<namespace>/<ingress_name>:<number>".
	// For example, default/routetest:0.
	portNameSplits := strings.Split(server.Port.Name, ":")
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == ing.GetNamespace()+"/"+ing.GetName()
}

// SortServers sorts `Server` according to its port name.
func SortServers(servers []*istiov1alpha3.Server) []*istiov1alpha3.Server {
	sort.Slice(servers, func(i, j int) bool {
		return strings.Compare(servers[i].Port.Name, servers[j].Port.Name) < 0
	})
	return servers
}

// MakeIngressGateways creates Gateways for a given Ingress.
func MakeIngressGateways(ctx context.Context, ing *v1alpha1.Ingress, originSecrets map[string]*corev1.Secret, svcLister corev1listers.ServiceLister) ([]*v1alpha3.Gateway, error) {
	gatewayServices, err := getGatewayServices(ctx, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1alpha3.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		gateway, err := makeIngressGateway(ctx, ing, originSecrets, gatewayService.Spec.Selector, gatewayService)
		if err != nil {
			return nil, err
		}
		gateways[i] = gateway
	}
	return gateways, nil
}

func makeIngressGateway(ctx context.Context, ing *v1alpha1.Ingress, originSecrets map[string]*corev1.Secret, selector map[string]string, gatewayService *corev1.Service) (*v1alpha3.Gateway, error) {
	ns := ing.GetNamespace()
	if len(ns) == 0 {
		ns = system.Namespace()
	}
	servers, err := MakeTLSServers(ing, gatewayService.Namespace, originSecrets)
	if err != nil {
		return nil, err
	}
	hosts := sets.String{}
	for _, rule := range ing.Spec.Rules {
		hosts.Insert(rule.Hosts...)
	}
	servers = append(servers, MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol, hosts.List()))
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:            GatewayName(ing, gatewayService),
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Labels: map[string]string{
				// We need this label to find out all of Gateways of a given Ingress.
				networking.IngressLabelKey: ing.GetName(),
			},
		},
		Spec: istiov1alpha3.Gateway{
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

// GatewayName create a name for the Gateway that is built based on the given Ingress and bonds to the
// given ingress gateway service.
func GatewayName(accessor kmeta.Accessor, gatewaySvc *corev1.Service) string {
	gatewayServiceKey := fmt.Sprintf("%s/%s", gatewaySvc.Namespace, gatewaySvc.Name)
	return fmt.Sprintf("%s-%d", accessor.GetName(), adler32.Checksum([]byte(gatewayServiceKey)))
}

// MakeTLSServers creates the expected Gateway TLS `Servers` based on the given Ingress.
func MakeTLSServers(ing *v1alpha1.Ingress, gatewayServiceNamespace string, originSecrets map[string]*corev1.Secret) ([]*istiov1alpha3.Server, error) {
	servers := make([]*istiov1alpha3.Server, len(ing.Spec.TLS))
	// TODO(zhiminx): for the hosts that does not included in the IngressTLS but listed in the IngressRule,
	// do we consider them as hosts for HTTP?
	for i, tls := range ing.Spec.TLS {
		credentialName := tls.SecretName
		// If the origin secret is not in the target namespace, then it should have been
		// copied into the target namespace. So we use the name of the copy.
		if tls.SecretNamespace != gatewayServiceNamespace {
			originSecret, ok := originSecrets[secretKey(tls)]
			if !ok {
				return nil, fmt.Errorf("unable to get the original secret %s/%s", tls.SecretNamespace, tls.SecretName)
			}
			credentialName = targetSecret(originSecret, ing)
		}

		port := ing.GetNamespace() + "/" + ing.GetName()

		servers[i] = &istiov1alpha3.Server{
			Hosts: tls.Hosts,
			Port: &istiov1alpha3.Port{
				Name:     fmt.Sprintf("%s:%d", port, i),
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.Server_TLSOptions{
				Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    credentialName,
			},
		}
	}
	return SortServers(servers), nil
}

// MakeHTTPServer creates a HTTP Gateway `Server` based on the HTTPProtocol
// configureation.
func MakeHTTPServer(httpProtocol network.HTTPProtocol, hosts []string) *istiov1alpha3.Server {
	if httpProtocol == network.HTTPDisabled {
		return nil
	}
	server := &istiov1alpha3.Server{
		Hosts: hosts,
		Port: &istiov1alpha3.Port{
			Name:     httpServerPortName,
			Number:   80,
			Protocol: "HTTP",
		},
	}
	if httpProtocol == network.HTTPRedirected {
		server.Tls = &istiov1alpha3.Server_TLSOptions{
			HttpsRedirect: true,
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
func UpdateGateway(gateway *v1alpha3.Gateway, want []*istiov1alpha3.Server, existing []*istiov1alpha3.Server) *v1alpha3.Gateway {
	existingServers := sets.String{}
	for i := range existing {
		existingServers.Insert(existing[i].Port.Name)
	}

	servers := []*istiov1alpha3.Server{}
	for _, server := range gateway.Spec.Servers {
		// We remove
		//  1) the existing servers
		//  2) the default HTTP server and HTTPS server in the gateway because they are only used for the scenario of not reconciling gateway.
		//  3) the placeholder servers.
		if existingServers.Has(server.Port.Name) || isDefaultServer(server) || isPlaceHolderServer(server) {
			continue
		}
		servers = append(servers, server)
	}
	servers = append(servers, want...)

	// Istio Gateway requires to have at least one server. So if the final gateway does not have any server,
	// we add "placeholder" server back.
	if len(servers) == 0 {
		servers = append(servers, &placeholderServer)
	}

	SortServers(servers)
	gateway.Spec.Servers = servers
	return gateway
}

func isDefaultServer(server *istiov1alpha3.Server) bool {
	if server.Port.Name == "https" {
		return len(server.Hosts) > 0 && server.Hosts[0] == "*"
	}
	return server.Port.Name == "http"
}

func isPlaceHolderServer(server *istiov1alpha3.Server) bool {
	return equality.Semantic.DeepEqual(server, &placeholderServer)
}
