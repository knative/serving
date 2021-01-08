/*
Copyright 2020 The Knative Authors

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// MakeDomainClaim creates a ClusterDomainClaim named after the given DomainMapping
// and giving ownership of the domain name to the DomainMapping's namespace.
func MakeDomainClaim(dm *v1alpha1.DomainMapping) *netv1alpha1.ClusterDomainClaim {
	return &netv1alpha1.ClusterDomainClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: dm.Name,
		},
		Spec: netv1alpha1.ClusterDomainClaimSpec{
			Namespace: dm.Namespace,
		},
	}
}
