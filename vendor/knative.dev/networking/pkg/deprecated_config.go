/*
Copyright 2022 The Knative Authors

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

package pkg

import (
	corev1 "k8s.io/api/core/v1"
	"knative.dev/networking/pkg/config"
)

const (
	// ConfigName is the name of the configmap containing all
	// customizations for networking features.
	//
	// Deprecated: use knative.dev/networking/pkg/config.ConfigMapName
	ConfigName = config.ConfigMapName

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	//
	// Deprecated: use knative.dev/networking/pkg/config.DefaultDomainTemplate
	DefaultDomainTemplate = config.DefaultDomainTemplate

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	//
	// Deprecated: use knative.dev/networking/pkg/config.DefaultTagTemplate
	DefaultTagTemplate = config.DefaultTagTemplate

	// DefaultIngressClassKey is the name of the configuration entry
	// that specifies the default Ingress.
	//
	// Deprecated: use knative.dev/networking/pkg/config.DefaultIngressClassKey
	DefaultIngressClassKey = config.DefaultIngressClassKey

	// DefaultCertificateClassKey is the name of the configuration entry
	// that specifies the default Certificate.
	//
	// Deprecated: use knative.dev/networking/pkg/config.DefaultCertificateClassKey
	DefaultCertificateClassKey = config.DefaultCertificateClassKey

	// IstioIngressClassName value for specifying knative's Istio
	// Ingress reconciler.
	//
	// Deprecated: use knative.dev/networking/pkg/config.IstioIngressClassName
	IstioIngressClassName = config.IstioIngressClassName

	// CertManagerCertificateClassName value for specifying Knative's Cert-Manager
	// Certificate reconciler.
	//
	// Deprecated: use knative.dev/networking/pkg/config.CertManagerCertificateClassName
	CertManagerCertificateClassName = config.CertManagerCertificateClassName

	// DomainTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// Knative service's DNS name.
	//
	// Deprecated: use knative.dev/networking/pkg/config.DomainTemplateKey
	DomainTemplateKey = config.DomainTemplateKey

	// TagTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// hostname for a Route's tag.
	//
	// Deprecated: use knative.dev/networking/pkg/config.TagTemplateKey
	TagTemplateKey = config.TagTemplateKey

	// RolloutDurationKey is the name of the configuration entry
	// that specifies the default duration of the configuration rollout.
	//
	// Deprecated: use knative.dev/networking/pkg/config.RolloutDurationKey
	RolloutDurationKey = config.RolloutDurationKey

	// NamespaceWildcardCertSelectorKey is the name of the configuration
	// entry that specifies a LabelSelector to control which namespaces
	// have a wildcard certificate provisioned for them.
	//
	// Deprecated: use knative.dev/networking/pkg/config.NamespaceWildcardCertSelectorKey
	NamespaceWildcardCertSelectorKey = config.NamespaceWildcardCertSelectorKey

	// AutocreateClusterDomainClaimsKey is the key for the
	// AutocreateClusterDomainClaims property.
	//
	// Deprecated: use knative.dev/networking/pkg/config.AutocreateClusterDomainClaimsKey
	AutocreateClusterDomainClaimsKey = config.AutocreateClusterDomainClaimsKey

	// AutoTLSKey is the name of the configuration entry
	// that specifies enabling auto-TLS or not.
	//
	// Deprecated: use knative.dev/networking/pkg/config.AutoTLSKey
	AutoTLSKey = config.AutoTLSKey

	// HTTPProtocolKey is the name of the configuration entry that
	// specifies the HTTP endpoint behavior of Knative ingress.
	//
	// Deprecated: use knative.dev/networking/pkg/config.HTTPProtocolKey
	HTTPProtocolKey = config.HTTPProtocolKey

	// EnableMeshPodAddressabilityKey is the config for enabling pod addressability in mesh.
	//
	// Deprecated: use knative.dev/networking/pkg/config.EnableMeshPodAddressabilityKey
	EnableMeshPodAddressabilityKey = config.EnableMeshPodAddressabilityKey

	// MeshCompatibilityModeKey is the config for selecting the mesh compatibility mode.
	//
	// Deprecated: use knative.dev/networking/pkg/config.MeshCompatibilityModeKey
	MeshCompatibilityModeKey = config.MeshCompatibilityModeKey

	// DefaultExternalSchemeKey is the config for defining the scheme of external URLs.
	//
	// Deprecated: use knative.dev/networking/pkg/config.DefaultExternalSchemeKey
	DefaultExternalSchemeKey = config.DefaultExternalSchemeKey
)

// DomainTemplateValues are the available properties people can choose from
// in their Route's "DomainTemplate" golang template sting.
// We could add more over time - e.g. RevisionName if we thought that
// might be of interest to people.
//
// Deprecated: use knative.dev/networking/pkg/config.DomainTemplateValues
type DomainTemplateValues = config.DomainTemplateValues

// TagTemplateValues are the available properties people can choose from
// in their Route's "TagTemplate" golang template sting.
//
// Deprecated: use knative.dev/networking/pkg/config.TagTemplateValues
type TagTemplateValues = config.TagTemplateValues

// Config contains the networking configuration defined in the
// network config map.
//
// Deprecated: use knative.dev/networking/pkg/config.Config
type Config = config.Config

// HTTPProtocol indicates a type of HTTP endpoint behavior
// that Knative ingress could take.
//
// Deprecated: use knative.dev/networking/pkg/config.HTTPProtocol
type HTTPProtocol = config.HTTPProtocol

const (
	// HTTPEnabled represents HTTP protocol is enabled in Knative ingress.
	//
	// Deprecated: use knative.dev/networking/pkg/config.HTTPEnabled
	HTTPEnabled HTTPProtocol = config.HTTPEnabled

	// HTTPDisabled represents HTTP protocol is disabled in Knative ingress.
	//
	// Deprecated: use knative.dev/networking/pkg/config.HTTPDisabled
	HTTPDisabled HTTPProtocol = config.HTTPDisabled

	// HTTPRedirected represents HTTP connection is redirected to HTTPS in Knative ingress.
	//
	// Deprecated: use knative.dev/networking/pkg/config.HTTPRedirected
	HTTPRedirected HTTPProtocol = config.HTTPRedirected
)

// MeshCompatibilityMode is one of enabled (always use ClusterIP), disabled
// (always use Pod IP), or auto (try PodIP, and fall back to ClusterIP if mesh
// is detected).
//
// Deprecated: use knative.dev/networking/pkg/config.MeshCompatibilityMode
type MeshCompatibilityMode = config.MeshCompatibilityMode

const (
	// MeshCompatibilityModeEnabled instructs consumers of network plugins, such as
	// Knative Serving, to use ClusterIP when connecting to pods. This is
	// required when mesh is enabled (unless EnableMeshPodAddressability is set),
	// but is less efficient.
	//
	// Deprecated: Use knative.dev/networking/pkg/config/MeshCompatibilityModeEnabled
	MeshCompatibilityModeEnabled MeshCompatibilityMode = config.MeshCompatibilityModeEnabled

	// MeshCompatibilityModeDisabled instructs consumers of network plugins, such as
	// Knative Serving, to connect to individual Pod IPs. This is most efficient,
	// but will only work with mesh enabled when EnableMeshPodAddressability is
	// used.
	//
	// Deprecated: Use knative.dev/networking/pkg/config/MeshCompatibilityModeDisabled
	MeshCompatibilityModeDisabled MeshCompatibilityMode = config.MeshCompatibilityModeDisabled

	// MeshCompatibilityModeAuto instructs consumers of network plugins, such as
	// Knative Serving, to heuristically determine whether to connect using the
	// Cluster IP, or to ocnnect to individual Pod IPs. This is most efficient,
	// determine whether mesh is enabled, and fall back from Direct Pod IP
	// communication to Cluster IP as needed.
	//
	// Deprecated: Use knative.dev/networking/pkg/config/MeshCompatibilityModeAuto
	MeshCompatibilityModeAuto MeshCompatibilityMode = config.MeshCompatibilityModeAuto
)

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}

// NewConfigFromMap creates a Config from the supplied data.
//
// Deprecated: Use knative.dev/networking/pkg/config/NewConfigFromMap
var NewConfigFromMap = config.NewConfigFromMap
