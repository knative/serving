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
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	NetworkConfigName = "config-network"

	// IstioOutboundIPRangesKey is the name of the configuration entry
	// that specifies Istio outbound ip ranges.
	IstioOutboundIPRangesKey = "istio.sidecar.includeOutboundIPRanges"
)

// Network contains the networking configuration defined in the
// network config map.
type Network struct {
	// IstioOutboundIPRange specifies the IP ranges to intercept
	// by Istio sidecar.
	IstioOutboundIPRanges string
}

func validateOutboundIPRanges(s string) error {
	// * is a valid value
	if s == "*" {
		return nil
	}
	cidrs := strings.Split(s, ",")
	for _, cidr := range cidrs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return err
		}
	}
	return nil
}

// NewNetworkFromConfigMap creates a Network from the supplied ConfigMap
func NewNetworkFromConfigMap(configMap *corev1.ConfigMap) (*Network, error) {
	nc := &Network{}
	if ipr, ok := configMap.Data[IstioOutboundIPRangesKey]; !ok {
		// It is OK for this to be absent, we will elide the annotation.
	} else if err := validateOutboundIPRanges(ipr); err != nil {
		return nil, err
	} else {
		nc.IstioOutboundIPRanges = ipr
	}
	return nc, nil
}
