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

	"github.com/knative/pkg/kmeta"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/resources/names"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeCertificates creates Certificate array for the Route to request TLS certificates.
// Returns one certificate for each domain, that is, each tag and the major
func MakeCertificates(logger *zap.SugaredLogger, route *v1alpha1.Route, dnsNames []string, tags []string) []*networkingv1alpha1.Certificate {
	// TODO: why do we pass route here? we only need its namespace and uid?
	// or should we factor the logic of getting the dnsnames and tags here instead of in reconcile func in route.go
	// TODO: here it assumes dnsNames and tags have the same length, and is one to one mapping
	// do need to do valid check?

	var certs []*networkingv1alpha1.Certificate
	logger.Info("dnsNames: ", dnsNames, " ", len(dnsNames))
	for i, dnsName := range dnsNames {
		certs = append(certs, &networkingv1alpha1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				// Name:            names.Certificate(route), // TODO: does this return the same name?
				// TODO: fmt.Sprintf("%s-%s", names.Certificate(route), dnsName) this name is too long and kube support cert name
				// only up to 63 chars, we may want to rewrite the name.Certificate function altogether?

				// kube system supports cert name only up to 63 characters
				// we decided to adapt the scheme route-[UID]-[tag digest]
				// where route-UID will take 42 characters and leaves 20 characters for tag digest (need to count `-`)
				// we use https://golang.org/pkg/hash/adler32/#Checksum to compute the digest which result a positive int of 4 bytes
				// we represent the digest in integer format gives maximum value of 4,294,967,295 which is 10 digits

				// TODO: do we want to compute the checksum here or in the names.Certificate function
				// should be fine to put in the names.Certificate func, looks like it's not used elsewhere
				// TODO: only compute checksum if there's a tag
				Name:            fmt.Sprintf("%s-%d", names.Certificate(route), adler32.Checksum([]byte(tags[i]))),
				Namespace:       route.Namespace,
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
			},
			Spec: networkingv1alpha1.CertificateSpec{
				DNSNames: []string{dnsName},
				// SecretName: names.Certificate(route),
				SecretName: fmt.Sprintf("%s-%d", names.Certificate(route), adler32.Checksum([]byte(tags[i]))),
			},
		})
	}
	return certs
}
