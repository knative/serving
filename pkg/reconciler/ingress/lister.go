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
	"strconv"

	"go.uber.org/zap"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"
	"knative.dev/serving/pkg/network/ingress"
	"knative.dev/serving/pkg/network/status"
)

// NewStatusProber creates a new instance of status.Prober
func NewStatusProber(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	endpointsLister corev1listers.EndpointsLister,
	serviceLister corev1listers.ServiceLister,
	readyCallback func(*v1alpha1.Ingress)) *status.Prober {
	return status.NewProber(
		logger,
		&gatewayPodTargetLister{
			logger:          logger,
			gatewayLister:   gatewayLister,
			endpointsLister: endpointsLister,
			serviceLister:   serviceLister,
		},
		readyCallback,
	)
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

func (l *gatewayPodTargetLister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) (map[string]map[string]sets.String, error) {
	results := map[string]map[string]sets.String{}

	qualifiedGatewayNames := qualifiedGatewayNamesFromContext(ctx)

	gatewayHosts := ingress.HostsPerVisibility(ing, qualifiedGatewayNames)
	for gatewayName, hosts := range gatewayHosts {
		gateway, err := l.getGateway(gatewayName)
		if err != nil {
			return nil, fmt.Errorf("failed to get Gateway %q: %w", gatewayName, err)
		}
		targetsPerPod, err := l.listGatewayTargetsPerPods(gateway)
		if err != nil {
			return nil, fmt.Errorf("failed to list the probing URLs of Gateway %q: %w", gatewayName, err)
		}
		if len(targetsPerPod) == 0 {
			continue
		}
		for ip, targets := range targetsPerPod {
			if _, existed := results[ip]; !existed {
				results[ip] = map[string]sets.String{}
			}
			for _, host := range hosts.List() {
				for _, target := range targets {
					url := fmt.Sprintf(target.urlTmpl, host)

					if targets, existed := results[ip][target.targetPort]; existed {
						targets.Insert(url)
					} else {
						results[ip][target.targetPort] = sets.NewString(url)
					}
				}
			}
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

// listGatewayPodsURLs returns a map where the keys are the Gateway Pod IPs and the values are the corresponding
// URL templates and the Gateway Pod Port to be probed.
func (l *gatewayPodTargetLister) listGatewayTargetsPerPods(gateway *v1alpha3.Gateway) (map[string][]probeTarget, error) {
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

	targetsPerPods := make(map[string][]probeTarget)
	for _, server := range gateway.Spec.Servers {
		var urlTmpl string
		switch server.Port.Protocol {
		case "HTTP", "HTTP2":
			if server.Tls == nil || !server.Tls.HttpsRedirect {
				urlTmpl = "http://%%s:%d/"
			} else {
				continue
			}
		case "HTTPS":
			urlTmpl = "https://%%s:%d/"
		default:
			l.logger.Infof("Skipping Server %q because protocol %q is not supported", server.Port.Name, server.Port.Protocol)
			continue
		}

		portName, err := findNameForPortNumber(service, int32(server.Port.Number))
		if err != nil {
			l.logger.Infof("Skipping Server %q because Service %s/%s doesn't contain a port %d", server.Port.Name, service.Namespace, service.Name, server.Port.Number)
			continue
		}
		for _, sub := range endpoints.Subsets {
			portNumber, err := findPortNumberForName(sub, portName)
			if err != nil {
				l.logger.Infof("Skipping Subset %v because it doesn't contain a port %q", sub.Addresses, portName)
				continue
			}

			for _, addr := range sub.Addresses {
				targetsPerPods[addr.IP] = append(targetsPerPods[addr.IP], probeTarget{urlTmpl: fmt.Sprintf(urlTmpl, server.Port.Number), targetPort: strconv.Itoa(int(portNumber))})
			}
		}
	}
	return targetsPerPods, nil
}

// findNameForPortNumber finds the name for a given port as defined by a Service.
func findNameForPortNumber(svc *corev1.Service, portNumber int32) (string, error) {
	for _, port := range svc.Spec.Ports {
		if port.Port == portNumber {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("no port with number %d found", portNumber)
}

// findPortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
func findPortNumberForName(sub corev1.EndpointSubset, portName string) (int32, error) {
	for _, subPort := range sub.Ports {
		if subPort.Name == portName {
			return subPort.Port, nil
		}
	}
	return 0, fmt.Errorf("no port for name %q found", portName)
}
