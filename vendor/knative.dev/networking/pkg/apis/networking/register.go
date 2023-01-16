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

import "knative.dev/pkg/kmap"

const (
	// GroupName is the name for the networking API group.
	GroupName = "networking.internal.knative.dev"

	// CertifcateUIDLabelKey is used to specify a label selector for informers listing ingress secrets.
	CertificateUIDLabelKey = GroupName + "/certificate-uid"

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
)

const (
	// PublicGroupName is the name for hte public networking API group
	PublicGroupName = "networking.knative.dev"

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
	CertificateClassAnnotationKey = PublicGroupName + "/certificate.class"

	// CertificateClassAnnotationAltKey is an alternative casing to CertificateClassAnnotationKey
	//
	// This annotation is meant to be applied to Knative Services or Routes. Serving
	// will translate this to original casing for better compatibility with different
	// certificate providers
	CertificateClassAnnotationAltKey = PublicGroupName + "/certificate-class"

	// DisableAutoTLSAnnotationKey is the annotation key attached to a Knative Service/DomainMapping
	// to indicate that AutoTLS should not be enabled for it.
	DisableAutoTLSAnnotationKey = PublicGroupName + "/disableAutoTLS"

	// DisableAutoTLSAnnotationAltKey is an alternative casing to DisableAutoTLSAnnotationKey
	DisableAutoTLSAnnotationAltKey = PublicGroupName + "/disable-auto-tls"

	// HTTPOptionAnnotationKey is the annotation key attached to a Knative Service/DomainMapping
	// to indicate the HTTP option of it.
	HTTPOptionAnnotationKey = PublicGroupName + "/httpOption"

	// HTTPProtocolAnnotationKey is an alternative to HTTPOptionAnnotationKey
	HTTPProtocolAnnotationKey = PublicGroupName + "/http-protocol"

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
	IngressClassAnnotationKey = PublicGroupName + "/ingress.class"

	// IngressClassAnnotationAltKey is an alternative casing to IngressClassAnnotationKey
	//
	// This annotation is meant to be applied to Knative Services or Routes. Serving
	// will translate this to original casing for better compatibility with different
	// ingress providers
	IngressClassAnnotationAltKey = PublicGroupName + "/ingress-class"

	// WildcardCertDomainLabelKey is the label key attached to a certificate to indicate the
	// domain for which it was issued.
	WildcardCertDomainLabelKey = PublicGroupName + "/wildcardDomain"

	// VisibilityLabelKey is the label to indicate visibility of Route
	// and KServices.  It can be an annotation too but since users are
	// already using labels for domain, it probably best to keep this
	// consistent.
	VisibilityLabelKey = PublicGroupName + "/visibility"
)

// Pseudo-constants
var (
	// DefaultRetryCount will be set if Attempts not specified.
	DefaultRetryCount = 3

	IngressClassAnnotation = kmap.KeyPriority{
		IngressClassAnnotationKey,
		IngressClassAnnotationAltKey,
	}

	CertificateClassAnnotation = kmap.KeyPriority{
		CertificateClassAnnotationKey,
		CertificateClassAnnotationAltKey,
	}

	DisableAutoTLSAnnotation = kmap.KeyPriority{
		DisableAutoTLSAnnotationKey,
		DisableAutoTLSAnnotationAltKey,
	}

	HTTPProtocolAnnotation = kmap.KeyPriority{
		HTTPOptionAnnotationKey,
		HTTPProtocolAnnotationKey,
	}
)

func GetIngressClass(annotations map[string]string) (val string) {
	return IngressClassAnnotation.Value(annotations)
}

func GetCertificateClass(annotations map[string]string) (val string) {
	return CertificateClassAnnotation.Value(annotations)
}

func GetHTTPProtocol(annotations map[string]string) (val string) {
	return HTTPProtocolAnnotation.Value(annotations)
}

func GetDisableAutoTLS(annotations map[string]string) (val string) {
	return DisableAutoTLSAnnotation.Value(annotations)
}
