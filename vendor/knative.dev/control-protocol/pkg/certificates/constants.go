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
	Organization           = "knative.dev"
	LegacyFakeDnsName      = "data-plane." + Organization
	FakeDnsName            = LegacyFakeDnsName // Deprecated
	dataPlaneUserPrefix    = "kn-user-"
	dataPlaneRoutingPrefix = "kn-routing-"
	ControlPlaneName       = "kn-control"

	//These keys are meant to line up with cert-manager, see
	//https://cert-manager.io/docs/usage/certificate/#additional-certificate-output-formats
	CaCertName     = "ca.crt"
	CertName       = "tls.crt"
	PrivateKeyName = "tls.key"

	//These should be able to be deprecated some time in the future when the new names are fully adopted
	SecretCaCertKey = "ca-cert.pem"
	SecretCertKey   = "public-cert.pem"
	SecretPKKey     = "private-key.pem"
)

// DataPlaneRoutingName constructs a san for a data-plane-routing certificate
// Accepts a routingId  - a unique identifier used as part of the san (default is "0" used when an empty routingId is provided)
func DataPlaneRoutingName(routingId string) string {
	if routingId == "" {
		routingId = "0"
	}
	return dataPlaneRoutingPrefix + strings.ToLower(routingId)
}

// DataPlaneUserName constructs a san for a data-plane-user certificate
// Accepts a namespace  - the namespace for which the certificate was created
func DataPlaneUserName(namespace string) string {
	return dataPlaneUserPrefix + strings.ToLower(namespace)
}
