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

// Istio contains istio related configuration defined in the
// istio config map.
type Istio struct {
	// IngressGateway specifies the ingress gateway url.
	IngressGateway string
}

// NewIstioFromConfigMap creates an Istio config from the supplied ConfigMap
func NewIstioFromConfigMap(configMap *corev1.ConfigMap) (*Istio, error) {
	gateway, ok := configMap.Data[IngressGatewayKey]
	if !ok {
		return nil, fmt.Errorf("failed to fetch %s from configmap %s", IngressGatewayKey, IstioConfigName)
	}
	if errs := validation.IsDNS1123Subdomain(gateway); len(errs) > 0 {
		return nil, fmt.Errorf("invalid gateway format: %v", errs)
	}

	return &Istio{
		IngressGateway: gateway,
	}, nil
}
