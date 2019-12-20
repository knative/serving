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

package ingress

import (
	"context"
	"fmt"
	"net/url"
	neturl "net/url"
	"strconv"

	"go.uber.org/zap"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/ingress"
	"knative.dev/serving/pkg/network/status"
)

func NewProbeTargetLister(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	endpointsLister corev1listers.EndpointsLister,
	serviceLister corev1listers.ServiceLister) status.ProbeTargetLister {
	return &gatewayPodTargetLister{
		logger:          logger,
		gatewayLister:   gatewayLister,
		endpointsLister: endpointsLister,
		serviceLister:   serviceLister,
	}
}

type gatewayPodTargetLister struct {
	logger *zap.SugaredLogger

	gatewayLister   istiolisters.GatewayLister
	endpointsLister corev1listers.EndpointsLister
	serviceLister   corev1listers.ServiceLister
}

// probeTargets represents the target to probe.
type probeTarget struct {
	// urlTmpl is an url template to probe.
	urlTmpl string
	// targetPort is a port of Gateway pod.
	targetPort string
}

func (l *gatewayPodTargetLister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) ([]status.ProbeTarget, error) {
	results := []status.ProbeTarget{}

	qualifiedGatewayNames := qualifiedGatewayNamesFromContext(ctx)
	gatewayHosts := ingress.HostsPerVisibility(ing, qualifiedGatewayNames)
	for gatewayName, hosts := range gatewayHosts {
		gateway, err := l.getGateway(gatewayName)
		if err != nil {
			return nil, fmt.Errorf("failed to get Gateway %q: %w", gatewayName, err)
		}
		targets, err := l.listGatewayTargets(gateway)
		if err != nil {
			return nil, fmt.Errorf("failed to list the probing URLs of Gateway %q: %w", gatewayName, err)
		}
		if len(targets) == 0 {
			continue
		}
		for _, target := range targets {
			qualifiedTarget := status.ProbeTarget{
				PodIPs: target.PodIPs,
				Port:   target.Port,
				URLs:   []neturl.URL{},
			}
			// Use sorted host for consistent ordering.
			for _, host := range hosts.List() {
				newURL := target.URLs[0]
				newURL.Host = fmt.Sprintf("%s:%s", host, target.Port)
				qualifiedTarget.URLs = append(qualifiedTarget.URLs, newURL)
			}
			results = append(results, qualifiedTarget)
		}
	}
	return results, nil
}

func (l *gatewayPodTargetLister) getGateway(name string) (*v1alpha3.Gateway, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Gateway name %q: %w", name, err)
	}
	if namespace == "" {
		return nil, fmt.Errorf("unexpected unqualified Gateway name %q", name)
	}
	return l.gatewayLister.Gateways(namespace).Get(name)
}

// listGatewayPodsURLs returns a probe targets for a given Gateway.
func (l *gatewayPodTargetLister) listGatewayTargets(gateway *v1alpha3.Gateway) ([]status.ProbeTarget, error) {
	selector := labels.SelectorFromSet(gateway.Spec.Selector)

	services, err := l.serviceLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list Services: %w", err)
	}
	if len(services) == 0 {
		l.logger.Infof("Skipping Gateway %s/%s because it has no corresponding Service", gateway.Namespace, gateway.Name)
		return nil, nil
	}
	service := services[0]

	endpoints, err := l.endpointsLister.Endpoints(service.Namespace).Get(service.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get Endpoints: %w", err)
	}

	targets := []status.ProbeTarget{}
	for _, server := range gateway.Spec.Servers {
		url := url.URL{}
		switch server.Port.Protocol {
		case "HTTP", "HTTP2":
			if server.Tls != nil && server.Tls.HttpsRedirect {
				// ignoring HTTPS redirects.
				continue
			}
			url.Scheme = "http"
		case "HTTPS":
			url.Scheme = "https"
		default:
			l.logger.Infof("Skipping Server %q because protocol %q is not supported", server.Port.Name, server.Port.Protocol)
			continue
		}

		portName, err := network.NameForPortNumber(service, int32(server.Port.Number))
		if err != nil {
			l.logger.Infof("Skipping Server %q because Service %s/%s doesn't contain a port %d", server.Port.Name, service.Namespace, service.Name, server.Port.Number)
			continue
		}
		for _, sub := range endpoints.Subsets {
			portNumber, err := network.PortNumberForName(sub, portName)
			if err != nil {
				l.logger.Infof("Skipping Subset %v because it doesn't contain a port %q", sub.Addresses, portName)
				continue
			}
			target := status.ProbeTarget{
				PodIPs: sets.NewString(),
				Port:   strconv.Itoa(int(portNumber)),
				URLs:   []neturl.URL{url},
			}
			for _, addr := range sub.Addresses {
				target.PodIPs.Insert(addr.IP)
			}
			targets = append(targets, target)
		}
	}
	return targets, nil
}
