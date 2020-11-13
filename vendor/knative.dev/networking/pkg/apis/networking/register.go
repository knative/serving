/*
Copyright 2018 The Knative Authors

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

package networking

const (
	// GroupName is the name for the networking API group.
	GroupName = "networking.internal.knative.dev"

	// IngressClassAnnotationKey is the annotation for the
	// explicit class of Ingress that a particular resource has
	// opted into. For example,
	//
	//    networking.knative.dev/ingress.class: some-network-impl
	//
	// This uses a different domain because unlike the resource, it is
	// user-facing.
	//
	// The parent resource may use its own annotations to choose the
	// annotation value for the Ingress it uses.  Based on such
	// value a different reconciliation logic may be used (for examples,
	// Istio-based Ingress will reconcile into a VirtualService).
	IngressClassAnnotationKey = "networking.knative.dev/ingress.class"

	// DisableAutoTLSAnnotationKey is the label key attached to a namespace to indicate that
	// AutoTLS should not be enabled for it.
	DisableAutoTLSAnnotationKey = "networking.knative.dev/disableAutoTLS"
	// IngressLabelKey is the label key attached to underlying network programming
	// resources to indicate which Ingress triggered their creation.
	IngressLabelKey = GroupName + "/ingress"

	// OriginSecretNameLabelKey is the label key attached to the TLS secret to indicate
	// the name of the origin secret that the TLS secret is copied from.
	OriginSecretNameLabelKey = GroupName + "/originSecretName"

	// OriginSecretNamespaceLabelKey is the label key attached to the TLS secret
	// to indicate the namespace of the origin secret that the TLS secret is copied from.
	OriginSecretNamespaceLabelKey = GroupName + "/originSecretNamespace"

	// RolloutAnnotationKey is the annotation key for storing
	// the rollout state in the Annotations of the Kingress or Route.Status.
	RolloutAnnotationKey = GroupName + "/rollout"

	// CertificateClassAnnotationKey is the annotation for the
	// explicit class of Certificate that a particular resource has
	// opted into. For example,
	//
	//    networking.knative.dev/certificate.class: some-network-impl
	//
	// This uses a different domain because unlike the resource, it is
	// user-facing.
	//
	// The parent resource may use its own annotations to choose the
	// annotation value for the Certificate it uses.  Based on such
	// value a different reconciliation logic may be used (for examples,
	// Cert-Manager-based Certificate will reconcile into a Cert-Manager Certificate).
	CertificateClassAnnotationKey = "networking.knative.dev/certificate.class"

	// DeprecatedDisableWildcardCertLabelKey is the deprecated label key attached to a namespace to indicate that
	// a wildcard certificate should be not created for it.
	DeprecatedDisableWildcardCertLabelKey = GroupName + "/disableWildcardCert"

	// DisableWildcardCertLabelKey is the label key attached to a namespace to indicate that
	// a wildcard certificate should be not created for it.
	DisableWildcardCertLabelKey = "networking.knative.dev/disableWildcardCert"

	// WildcardCertDomainLabelKey is the label key attached to a certificate to indicate the
	// domain for which it was issued.
	WildcardCertDomainLabelKey = "networking.knative.dev/wildcardDomain"
)

// Pseudo-constants
var (
	// DefaultRetryCount will be set if Attempts not specified.
	DefaultRetryCount = 3
)
