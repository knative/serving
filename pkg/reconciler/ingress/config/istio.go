/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
)

const (
	// IstioConfigName is the name of the configmap containing all
	// customizations for istio related features.
	IstioConfigName = "config-istio"

	// GatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public Ingresses.
	GatewayKeyPrefix = "gateway."

	// LocalGatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public & private Ingresses.
	LocalGatewayKeyPrefix = "local-gateway."

	// ReconcileExternalGatewayKey the is the name of the configuration entry that specifies
	// reconciling external Istio Gateways or not.
	ReconcileExternalGatewayKey = "reconcileExternalGateway"

	// defaultReconcileGateway is the default value of reconcileExternalGateway.
	defaultReconcileGateway = false
)

func defaultGateways() []Gateway {
	return []Gateway{{
		Namespace: system.Namespace(),
		Name:      "knative-ingress-gateway",
		ServiceURL: fmt.Sprintf("istio-ingressgateway.istio-system.svc.%s",
			network.GetClusterDomainName()),
	}}
}

func defaultLocalGateways() []Gateway {
	return []Gateway{{
		Namespace: system.Namespace(),
		Name:      "cluster-local-gateway",
		ServiceURL: fmt.Sprintf("cluster-local-gateway.istio-system.svc.%s",
			network.GetClusterDomainName()),
	}}
}

// Gateway specifies the name of the Gateway and the K8s Service backing it.
type Gateway struct {
	Namespace  string
	Name       string
	ServiceURL string
}

// QualifiedName returns gateway name in '{namespace}/{name}' format.
func (g Gateway) QualifiedName() string {
	return g.Namespace + "/" + g.Name
}

// Istio contains istio related configuration defined in the
// istio config map.
type Istio struct {
	// IngressGateway specifies the gateway urls for public Ingress.
	IngressGateways []Gateway

	// LocalGateway specifies the gateway urls for public & private Ingress.
	LocalGateways []Gateway

	// ReconcileExternalGateway specifies if external Istio Gateways will be reconciled or not.
	ReconcileExternalGateway bool
}

func parseGateways(configMap *corev1.ConfigMap, prefix string) ([]Gateway, error) {
	urls := map[string]string{}
	gatewayNames := []string{}
	for k, v := range configMap.Data {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		gatewayName, serviceURL := k[len(prefix):], v
		if errs := validation.IsDNS1123Subdomain(serviceURL); len(errs) > 0 {
			return nil, fmt.Errorf("invalid gateway format: %w", errs)
		}
		gatewayNames = append(gatewayNames, gatewayName)
		urls[gatewayName] = serviceURL
	}
	sort.Strings(gatewayNames)
	gateways := make([]Gateway, len(gatewayNames))
	for i, gatewayName := range gatewayNames {
		gateways[i] = Gateway{
			Namespace:  system.Namespace(),
			Name:       gatewayName,
			ServiceURL: urls[gatewayName],
		}
	}
	return gateways, nil
}

// NewIstioFromConfigMap creates an Istio config from the supplied ConfigMap
func NewIstioFromConfigMap(configMap *corev1.ConfigMap) (*Istio, error) {
	gateways, err := parseGateways(configMap, GatewayKeyPrefix)
	if err != nil {
		return nil, err
	}
	if len(gateways) == 0 {
		gateways = defaultGateways()
	}
	localGateways, err := parseGateways(configMap, LocalGatewayKeyPrefix)
	if err != nil {
		return nil, err
	}
	if len(localGateways) == 0 {
		localGateways = defaultLocalGateways()
	}
	localGateways = removeMeshGateway(localGateways)
	reconcileGateway := defaultReconcileGateway
	if reconcileGatewayStr := configMap.Data[ReconcileExternalGatewayKey]; len(reconcileGatewayStr) != 0 {
		if reconcileGateway, err = strconv.ParseBool(reconcileGatewayStr); err != nil {
			return nil, err
		}
	}
	return &Istio{
		IngressGateways:          gateways,
		LocalGateways:            localGateways,
		ReconcileExternalGateway: reconcileGateway,
	}, nil
}

func removeMeshGateway(gateways []Gateway) []Gateway {
	gws := []Gateway{}
	for _, g := range gateways {
		if g.Name != "mesh" {
			gws = append(gws, g)
		}
	}
	return gws
}
