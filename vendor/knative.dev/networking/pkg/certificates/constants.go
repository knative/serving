/*
Copyright 2021 The Knative Authors

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

package certificates

import "strings"

const (
	Organization = "knative.dev"

	// nolint:all
	LegacyFakeDnsName = "data-plane." + Organization

	// nolint:all
	// Deprecated: FakeDnsName is deprecated.
	// Please use the DataPlaneRoutingSAN for calls to the Activator
	// and the DataPlaneUserSAN function for calls to a Knative-Service via Queue-Proxy.
	FakeDnsName = LegacyFakeDnsName

	dataPlaneUserPrefix = "kn-user-"
	DataPlaneRoutingSAN = "kn-routing"

	// These keys are meant to line up with cert-manager, see
	// https://cert-manager.io/docs/usage/certificate/#additional-certificate-output-formats
	CaCertName     = "ca.crt"
	CertName       = "tls.crt"
	PrivateKeyName = "tls.key"

	// These should be able to be deprecated some time in the future when the new names are fully adopted
	// #nosec
	// Deprecated: please use CaCertName instead.
	SecretCaCertKey = "ca-cert.pem"
	// #nosec
	// Deprecated: please use CertName instead.
	SecretCertKey = "public-cert.pem"
	// #nosec
	// Deprecated: please use PrivateKeyName instead.
	SecretPKKey = "private-key.pem"
)

// DataPlaneUserSAN constructs a SAN for a data-plane-user certificate in the
// target namespace of a Knative Service.
func DataPlaneUserSAN(namespace string) string {
	return dataPlaneUserPrefix + strings.ToLower(namespace)
}
