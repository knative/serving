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

package revision

import (
	"github.com/knative/serving/pkg"

	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// IstioOutboundIPRangesKey is the name of the configuration entry
	// that specifies Istio outbound ip ranges.
	IstioOutboundIPRangesKey = "istio.sidecar.includeOutboundIPRanges"
)

// NetworkConfig contains the networking configuration defined in the
// network config map.
type NetworkConfig struct {
	// IstioOutboundIPRange specifies the IP ranges to intercept
	// by Istio sidecar.
	IstioOutboundIPRanges string
}

// NewNetworkConfig creates a DomainConfig by reading the domain configmap from
// the supplied client.
func NewNetworkConfig(kubeClient kubernetes.Interface) (*NetworkConfig, error) {
	m, err := kubeClient.CoreV1().ConfigMaps(pkg.GetServingSystemNamespace()).Get(controller.GetNetworkConfigMapName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return NewNetworkConfigFromConfigMap(m), nil
}

// NewNetworkConfigFromConfigMap creates a NewNetworkConfig from the supplied ConfigMap
func NewNetworkConfigFromConfigMap(configMap *corev1.ConfigMap) *NetworkConfig {
	return &NetworkConfig{IstioOutboundIPRanges: configMap.Data[IstioOutboundIPRangesKey]}
}
