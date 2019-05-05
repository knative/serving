/*
Copyright 2019 The Knative Authors

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

package resources

import (
	"github.com/knative/pkg/kmeta"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/resources/names"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeCertificate creates Certificate for the Route to request TLS certificates.
func MakeCertificate(route *v1alpha1.Route, dnsNames []string) *networkingv1alpha1.Certificate {
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.Certificate(route),
			Namespace:       route.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: names.Certificate(route),
		},
	}
}
