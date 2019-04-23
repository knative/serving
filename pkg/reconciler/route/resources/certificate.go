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
	"context"
	"fmt"
	"strings"

	"github.com/knative/pkg/system"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/resources/names"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// MakeCertificates creates Certificates for the Route to request TLS certificates.
func MakeCertificates(ctx context.Context, route *v1alpha1.Route, dnsNames []string, enableWildcardCert bool) ([]*networkingv1alpha1.Certificate, error) {
	certs := []*networkingv1alpha1.Certificate{}
	if enableWildcardCert {
		wildcardDNSNames := sets.String{}
		for _, dnsName := range dnsNames {
			wildcardDNS := wildcard(dnsName)
			wildcardDNSNames.Insert(wildcardDNS)
		}

		for wildcardDNSName := range wildcardDNSNames {
			certName := wildcardCertName(wildcardDNSName)
			cert, err := makeCert(route, []string{wildcardDNSName}, certName)
			if err != nil {
				return nil, err
			}
			certs = append(certs, cert)
		}
	} else {
		// For non-wildcard certificate, it is Route-specific. Therefore, we generate
		// the certificate name based on Route information.
		cert, err := makeCert(route, dnsNames, names.Certificate(route))
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func makeCert(route *v1alpha1.Route, dnsNames []string, certName string) (*networkingv1alpha1.Certificate, error) {
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name: certName,
			// TODO(zhiminx): make certificate namespace configurable
			Namespace: system.Namespace(),
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: certName,
		},
	}, nil
}

func wildcard(dnsName string) string {
	splits := strings.Split(dnsName, ".")
	return fmt.Sprintf("*.%s", strings.Join(splits[1:], "."))
}

func wildcardCertName(wildcardDNSName string) string {
	splits := strings.Split(wildcardDNSName, ".")
	return strings.Join(splits[1:], ".")
}
