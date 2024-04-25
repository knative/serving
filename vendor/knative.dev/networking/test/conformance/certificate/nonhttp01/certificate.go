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
	"context"
	"testing"

	"knative.dev/networking/test"
	utils "knative.dev/networking/test/conformance/certificate"
)

// TestSecret verifies that a certificate creates a secret
func TestSecret(t *testing.T) {
	ctx, clients := context.Background(), test.Setup(t)
	certName := test.ObjectNameForTest(t) + "." + test.NetworkingFlags.ServiceDomain

	cert := utils.CreateCertificate(ctx, t, clients, []string{certName})

	t.Logf("Waiting for Certificate %q to transition to Ready", cert.Name)
	if err := utils.WaitForCertificateState(ctx, clients.NetworkingClient, cert.Name, utils.IsCertificateReady, "CertificateIsReady"); err != nil {
		t.Fatal("Error waiting for the certificate to become ready for the latest revision:", err)
	}

	err := utils.WaitForCertificateSecret(ctx, t, clients, cert, t.Name())
	if err != nil {
		t.Error("Failed to wait for secret:", err)
	}
}
