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
	"fmt"
	"hash/adler32"
	"sort"

	"github.com/knative/pkg/kmeta"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/resources/names"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeCertificates creates an array of Certificate for the Route to request TLS certificates.
// domainTagMap is an one-to-one mapping between domain and tag, for major domain (tag-less),
// the value is an empty string
// Returns one certificate for each domain
func MakeCertificates(route *v1alpha1.Route, domainTagMap map[string]string) []*networkingv1alpha1.Certificate {
	order := make(sort.StringSlice, 0, len(domainTagMap))
	for dnsName := range domainTagMap {
		order = append(order, dnsName)
	}
	order.Sort()

	var certs []*networkingv1alpha1.Certificate
	for _, dnsName := range order {
		tag := domainTagMap[dnsName]

		// k8s supports cert name only up to 63 chars and so is constructed as route-[UID]-[tag digest]
		// where route-[UID] will take 42 characters and leaves 20 characters for tag digest (need to include `-`).
		// We use https://golang.org/pkg/hash/adler32/#Checksum to compute the digest which returns a uint32.
		// We represent the digest in unsigned integer format with maximum value of 4,294,967,295 which are 10 digits.
		// The "-[tag digest]" is computed only if there's a tag
		certName := names.Certificate(route)
		if tag != "" {
			certName = fmt.Sprintf("%s-%d", certName, adler32.Checksum([]byte(tag)))
		}

		certs = append(certs, &networkingv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:            certName,
				Namespace:       route.Namespace,
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
			},
			Spec: networkingv1alpha1.CertificateSpec{
				DNSNames:   []string{dnsName},
				SecretName: certName,
			},
		})
	}
	return certs
}
