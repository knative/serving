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

	err := utils.WaitForCertificateSecret(clients, cert, t.Name())
	if err != nil {
		t.Fatalf("failed to wait for secret: %v", err)
	}
}
