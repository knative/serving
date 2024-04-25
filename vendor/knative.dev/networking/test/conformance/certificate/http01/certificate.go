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

package http01

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	utils "knative.dev/networking/test/conformance/certificate"
)

// TestHTTP01Challenge verifies that HTTP challenges are created for a certificate
func TestHTTP01Challenge(t *testing.T) {
	subDomain := test.ObjectNameForTest(t)
	ctx, clients := context.Background(), test.Setup(t)

	certDomains := [][]string{
		{subDomain + ".knative-test.dev"},
		{subDomain + "2.knative-test.dev", subDomain + "3.knative-test.dev"},
	}

	for _, domains := range certDomains {
		cert := utils.CreateCertificate(ctx, t, clients, domains)

		if err := utils.WaitForCertificateState(ctx, clients.NetworkingClient, cert.Name,
			func(c *v1alpha1.Certificate) (bool, error) {
				for _, dnsName := range c.Spec.DNSNames {
					found := false

					for _, challenge := range c.Status.HTTP01Challenges {
						if challenge.URL.Host == dnsName {
							found = true
							break
						}
					}

					if !found {
						return false, nil
					}
				}

				return true, nil
			},
			t.Name()); err != nil {
			t.Fatal("failed to wait for HTTP01 challenges:", err)
		}

		cert, err := clients.NetworkingClient.Certificates.Get(ctx, cert.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal("failed to fetch certificate:", err)
		}

		utils.VerifyChallenges(ctx, t, clients, cert)
	}
}
