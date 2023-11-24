/*
Copyright 2023 The Knative Authors

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

package v1alpha1

import (
	"slices"
)

// GetIngressTLSForVisibility returns a list of `Spec.TLS` where each host in the `Rules.Hosts` field is
// present in `Spec.TLS.Hosts` and where the Rules have the defined ingress visibility.
// This method can be used in net-* implementations to select the correct `IngressTLS` entries
// for cluster-local and cluster-external gateways/listeners.
func (i *Ingress) GetIngressTLSForVisibility(visibility IngressVisibility) []IngressTLS {
	ingressTLS := make([]IngressTLS, 0, len(i.Spec.TLS))

	if i.Spec.TLS == nil || len(i.Spec.TLS) == 0 {
		return ingressTLS
	}

	for _, rule := range i.Spec.Rules {
		if rule.Visibility == visibility {
			if rule.Hosts == nil || len(rule.Hosts) == 0 {
				return ingressTLS
			}

			for _, tls := range i.Spec.TLS {
				containsAllRuleHosts := true
				for _, h := range rule.Hosts {
					if !slices.Contains(tls.Hosts, h) {
						containsAllRuleHosts = false
					}
				}
				if containsAllRuleHosts {
					ingressTLS = append(ingressTLS, tls)
				}
			}
		}
	}

	return ingressTLS
}
