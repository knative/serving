/*
Copyright 2019 The Knative Authors

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
	"fmt"
	"hash/adler32"
	"sort"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/config"
	"knative.dev/pkg/kmap"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/apis/serving"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	networkingv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/resources/names"
)

const (
	localDomainSuffix = "-local"
)

// MakeCertificate creates a Certificate, inheriting the certClass
// annotations from the owner, as well as the namespaces. If owner
// does not have a certClass, use the provided `certClass` parameter.
// baseDomain is the top level domain for the cert. It should be a suffix of dnsName.
func MakeCertificate(owner kmeta.OwnerRefableAccessor, ownerLabelKey string, dnsName string, certName string, certClass string, baseDomain string) *networkingv1alpha1.Certificate {
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            certName,
			Namespace:       owner.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(owner)},
			Annotations: kmeta.FilterMap(kmeta.UnionMaps(map[string]string{
				networking.CertificateClassAnnotationKey: certClass,
			}, owner.GetAnnotations()), ExcludedAnnotations.Has),
			Labels: map[string]string{
				ownerLabelKey:                      owner.GetName(),
				networking.CertificateTypeLabelKey: string(config.CertificateExternalDomain),
			},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   []string{dnsName},
			Domain:     baseDomain,
			SecretName: certName,
		},
	}
}

// MakeCertificates creates an array of Certificate for the Route to request TLS certificates.
// domainTagMap is an one-to-one mapping between domain and tag, for major domain (tag-less),
// the value is an empty string
// domain is the base domain to be used for the route. It should be a suffix of
// the domains in domainTagMap
// Returns one certificate for each domain
func MakeCertificates(route *v1.Route, domainTagMap map[string]string, certClass string, domain string) []*networkingv1alpha1.Certificate {
	order := make(sort.StringSlice, 0, len(domainTagMap))
	for dnsName := range domainTagMap {
		order = append(order, dnsName)
	}
	order.Sort()

	certs := make([]*networkingv1alpha1.Certificate, 0, len(order))
	for _, dnsName := range order {
		tag := domainTagMap[dnsName]
		certs = append(certs, MakeCertificate(route, serving.RouteLabelKey, dnsName, certNameFromRouteAndTag(route, tag), certClass, domain))
	}
	return certs
}

// MakeClusterLocalCertificate creates a Knative Certificate
// for cluster-local-domain-tls.
func MakeClusterLocalCertificate(route *v1.Route, tag string, domains sets.Set[string], certClass string) *networkingv1alpha1.Certificate {
	domainsOrdered := make(sort.StringSlice, 0, len(domains))
	for dnsName := range domains {
		domainsOrdered = append(domainsOrdered, dnsName)
	}
	domainsOrdered.Sort()

	certName := certNameFromRouteAndTag(route, tag) + localDomainSuffix

	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            certName,
			Namespace:       route.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
			Annotations: kmap.Filter(kmap.Union(map[string]string{
				networking.CertificateClassAnnotationKey: certClass,
			}, route.GetAnnotations()), ExcludedAnnotations.Has),
			Labels: map[string]string{
				serving.RouteLabelKey:              route.GetName(),
				networking.CertificateTypeLabelKey: string(config.CertificateClusterLocalDomain),
			},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   domainsOrdered,
			Domain:     "svc." + network.GetClusterDomainName(),
			SecretName: certName,
		},
	}
}

// certNameFromRouteAndTag returns a possibly shortended certName as
// k8s supports cert name only up to 63 chars and so is constructed as route-[UID]-[tag digest]
// where route-[UID] will take 42 characters and leaves 20 characters for tag digest (need to include `-`).
// We use https://golang.org/pkg/hash/adler32/#Checksum to compute the digest which returns a uint32.
// We represent the digest in unsigned integer format with maximum value of 4,294,967,295 which are 10 digits.
// The "-[tag digest]" is computed only if there's a tag
func certNameFromRouteAndTag(route *v1.Route, tag string) string {
	certName := names.Certificate(route)
	if tag != "" {
		certName += fmt.Sprint("-", adler32.Checksum([]byte(tag)))
	}
	return certName
}
