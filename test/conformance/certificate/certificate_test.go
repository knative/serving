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

package certificate

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/test"
)

// TestSecret verifies that a certificate creates a secret
func TestSecret(t *testing.T) {
	clients := test.Setup(t)
	certName := test.ObjectNameForTest(t) + ".example.com"

	cert, cancel := createCertificate(t, clients, []string{certName})
	defer cancel()

	waitForSecret(clients, cert.Spec.SecretName, t.Name())
}

// TestHttp01Challenge verifies that HTTP challenges are created for a certificate
func TestHttp01Challenge(t *testing.T) {
	subDomain := test.ObjectNameForTest(t)
	clients := test.Setup(t)

	certDomains := [][]string{
		{subDomain + ".example.com"},
		{subDomain + "2.example.com", subDomain + "3.example.com"},
	}

	for _, domains := range certDomains {
		cert, cancel := createCertificate(t, clients, domains)
		defer cancel()

		waitForCertificateState(clients.NetworkingClient, cert.Name, certHasHTTP01Challenges, t.Name())
		cert, err := clients.NetworkingClient.Certificates.Get(cert.Name, metav1.GetOptions{})

		if err != nil {
			t.Fatalf("Failed to fetch certificate: %v", err)
		}

		verifyChallenges(t, cert)
	}
}
