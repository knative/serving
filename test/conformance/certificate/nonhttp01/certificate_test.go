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

package nonhttp01

import (
	"testing"

	"knative.dev/serving/test"
	utils "knative.dev/serving/test/conformance/certificate"
)

// TestSecret verifies that a certificate creates a secret
func TestSecret(t *testing.T) {
	clients := test.Setup(t)
	certName := test.ObjectNameForTest(t) + ".example.com"

	cert, cancel := utils.CreateCertificate(t, clients, []string{certName})
	defer cancel()

	t.Logf("Waiting for Certificate %q to transition to Ready", cert.Name)
	if err := utils.WaitForCertificateState(clients.NetworkingClient, cert.Name, utils.IsCertificateReady, "CertificateIsReady"); err != nil {
		t.Fatalf("Error waiting for the certificate to become ready for the latest revision: %v", err)
	}

	err := utils.WaitForCertificateSecret(t, clients, cert, t.Name())
	if err != nil {
		t.Errorf("Failed to wait for secret: %v", err)
	}
}
