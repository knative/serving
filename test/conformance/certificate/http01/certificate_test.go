// +build e2e

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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
	utils "knative.dev/serving/test/conformance/certificate"
)

// TestHTTP01Challenge verifies that HTTP challenges are created for a certificate
func TestHTTP01Challenge(t *testing.T) {
	subDomain := test.ObjectNameForTest(t)
	clients := test.Setup(t)

	certDomains := [][]string{
		{subDomain + ".example.com"},
		{subDomain + "2.example.com", subDomain + "3.example.com"},
	}

	for _, domains := range certDomains {
		cert, cancel := utils.CreateCertificate(t, clients, domains)
		defer cancel()

		err := utils.WaitForCertificateState(clients.NetworkingClient, cert.Name,
			func(c *v1alpha1.Certificate) (bool, error) {
				return len(c.Status.HTTP01Challenges) == len(c.Spec.DNSNames), nil
			},
			t.Name())
		if err != nil {
			t.Fatalf("failed to wait for HTTP01 challenges: %v", err)
		}

		cert, err = clients.NetworkingClient.Certificates.Get(cert.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to fetch certificate: %v", err)
		}

		utils.VerifyChallenges(t, clients, cert)
	}
}
