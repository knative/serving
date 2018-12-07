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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// IstioConfigName is the name of the configmap containing all
	// customizations for istio related features.
	IstioConfigName = "config-istio"

	// IngressGatewayKey is the name of the configuration entry
	// that specifies ingress gateway url.
	IngressGatewayKey = "ingress-gateway"
)

// IngressGateway specifies the name of the Gateway and the K8s Service backing it.
type IngressGateway struct {
	GatewayName string
	ServiceURL  string
}

// Istio contains istio related configuration defined in the
// istio config map.
type Istio struct {
	// IngressGateway specifies the ingress gateway url.
	IngressGateways []IngressGateway
}

// NewIstioFromConfigMap creates an Istio config from the supplied ConfigMap
func NewIstioFromConfigMap(configMap *corev1.ConfigMap) (*Istio, error) {
	entries, ok := configMap.Data[IngressGatewayKey]
	if !ok {
		return nil, fmt.Errorf("failed to fetch %s from configmap %s", IngressGatewayKey, IstioConfigName)
	}
	gateways := []IngressGateway{}
	for _, entry := range strings.Split(entries, ";") {
		parts := strings.Split(entry, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid gateway entry format: %q", entry)
		}
		gatewayName, serviceURL := parts[0], parts[1]
		if errs := validation.IsDNS1123Subdomain(serviceURL); len(errs) > 0 {
			return nil, fmt.Errorf("invalid gateway format: %v", errs)
		}
		gateways = append(gateways,
			IngressGateway{
				GatewayName: gatewayName,
				ServiceURL:  serviceURL,
			})
	}
	return &Istio{
		IngressGateways: gateways,
	}, nil
}
