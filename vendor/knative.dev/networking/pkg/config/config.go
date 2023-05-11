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

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/lru"
	cm "knative.dev/pkg/configmap"
	"sigs.k8s.io/yaml"
)

var (
	templateCache *lru.Cache

	// Verify the default templates are valid.
	_ = template.Must(template.New("domain-template").Parse(DefaultDomainTemplate))
	_ = template.Must(template.New("tag-template").Parse(DefaultTagTemplate))
)

func init() {
	// The only failure is due to negative size.
	// Store ~10 latest templates per template type.
	templateCache = lru.New(10 * 2)
}

const (
	// ConfigName is the name of the configmap containing all
	// customizations for networking features.
	ConfigMapName = "config-network"

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	DefaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	DefaultTagTemplate = "{{.Tag}}-{{.Name}}"

	// IstioIngressClassName value for specifying knative's Istio
	// Ingress reconciler.
	IstioIngressClassName = "istio.ingress.networking.knative.dev"

	// CertManagerCertificateClassName value for specifying Knative's Cert-Manager
	// Certificate reconciler.
	CertManagerCertificateClassName = "cert-manager.certificate.networking.knative.dev"

	// ServingInternalCertName is the name of secret contains certificates in serving
	// system namespace.
	//
	// Deprecated: ServingInternalCertName is deprecated.
	// (use ServingControlCertName or ServingRoutingCertName instead)
	ServingInternalCertName = "knative-serving-certs"

	// ServingRoutingCertName is the name of secret contains certificates for Routing data in serving
	// system namespace. (Used by Ingress GWs and Activator)
	ServingRoutingCertName = "routing-serving-certs"

	// ServingControlCertName is the name of secret contains certificates for Control data in serving
	// system namespace. (Used by Autoscaler and Ingress control for example)
	ServingControlCertName = "control-serving-certs"
)

// Config Keys
const (

	// AutocreateClusterDomainClaimsKey is the key for the
	// AutocreateClusterDomainClaims property.
	AutocreateClusterDomainClaimsKey = "autocreate-cluster-domain-claims"

	// AutoTLSKey is the name of the configuration entry
	// that specifies enabling auto-TLS or not.
	AutoTLSKey = "auto-tls"

	// DefaultCertificateClassKey is the name of the configuration entry
	// that specifies the default Certificate.
	DefaultCertificateClassKey = "certificate-class"

	// DefaultExternalSchemeKey is the config for defining the scheme of external URLs.
	DefaultExternalSchemeKey = "default-external-scheme"

	// DefaultIngressClassKey is the name of the configuration entry
	// that specifies the default Ingress.
	DefaultIngressClassKey = "ingress-class"

	// DomainTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// Knative service's DNS name.
	DomainTemplateKey = "domain-template"

	// EnableMeshPodAddressabilityKey is the config for enabling pod addressability in mesh.
	EnableMeshPodAddressabilityKey = "enable-mesh-pod-addressability"

	// HTTPProtocolKey is the name of the configuration entry that
	// specifies the HTTP endpoint behavior of Knative ingress.
	HTTPProtocolKey = "http-protocol"

	// MeshCompatibilityModeKey is the config for selecting the mesh compatibility mode.
	MeshCompatibilityModeKey = "mesh-compatibility-mode"

	// NamespaceWildcardCertSelectorKey is the name of the configuration
	// entry that specifies a LabelSelector to control which namespaces
	// have a wildcard certificate provisioned for them.
	NamespaceWildcardCertSelectorKey = "namespace-wildcard-cert-selector"

	// RolloutDurationKey is the name of the configuration entry
	// that specifies the default duration of the configuration rollout.
	RolloutDurationKey = "rollout-duration"

	// TagTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// hostname for a Route's tag.
	TagTemplateKey = "tag-template"

	// InternalEncryptionKey is deprecated and replaced by InternalDataplaneTrustKey and internal-controlplane-trust
	// InternalEncryptionKey is the name of the configuration whether
	// internal traffic is encrypted or not.
	InternalEncryptionKey = "internal-encryption"

	// DataplaneTrustKey is the name of the configuration entry
	// defining the level of trust used for data plane traffic.
	DataplaneTrustKey = "dataplane-trust"

	// ControlplaneTrustKey is the name of the configuration entry
	// defining the level of trust used for control plane traffic.
	ControlplaneTrustKey = "controlplane-trust"
)

// HTTPProtocol indicates a type of HTTP endpoint behavior
// that Knative ingress could take.
type Trust string

const (
	// TrustDisabled - TLS not used
	TrustDisabled Trust = "disabled"

	// TrustMinimal - TLS used. We verify that the server is using Knative certificates
	TrustMinimal Trust = "minimal"

	// TrustEnabled - TLS used. We verify that the server is using Knative certificates of the right namespace
	TrustEnabled Trust = "enabled"

	// TrustMutual - same as TrustEnabled and we also verify the identity of the client.
	TrustMutual Trust = "mutual"

	// TrustIdentity - same as TrustMutual and we also add a trusted sender identity to the message.
	TrustIdentity Trust = "identity"
)

// HTTPProtocol indicates a type of HTTP endpoint behavior
// that Knative ingress could take.
type HTTPProtocol string

const (
	// HTTPEnabled represents HTTP protocol is enabled in Knative ingress.
	HTTPEnabled HTTPProtocol = "enabled"

	// HTTPDisabled represents HTTP protocol is disabled in Knative ingress.
	HTTPDisabled HTTPProtocol = "disabled"

	// HTTPRedirected represents HTTP connection is redirected to HTTPS in Knative ingress.
	HTTPRedirected HTTPProtocol = "redirected"
)

// MeshCompatibilityMode is one of enabled (always use ClusterIP), disabled
// (always use Pod IP), or auto (try PodIP, and fall back to ClusterIP if mesh
// is detected).
type MeshCompatibilityMode string

const (
	// MeshCompatibilityModeEnabled instructs consumers of network plugins, such as
	// Knative Serving, to use ClusterIP when connecting to pods. This is
	// required when mesh is enabled (unless EnableMeshPodAddressability is set),
	// but is less efficient.
	MeshCompatibilityModeEnabled MeshCompatibilityMode = "enabled"

	// MeshCompatibilityModeDisabled instructs consumers of network plugins, such as
	// Knative Serving, to connect to individual Pod IPs. This is most efficient,
	// but will only work with mesh enabled when EnableMeshPodAddressability is
	// used.
	MeshCompatibilityModeDisabled MeshCompatibilityMode = "disabled"

	// MeshCompatibilityModeAuto instructs consumers of network plugins, such as
	// Knative Serving, to heuristically determine whether to connect using the
	// Cluster IP, or to ocnnect to individual Pod IPs. This is most efficient,
	// determine whether mesh is enabled, and fall back from Direct Pod IP
	// communication to Cluster IP as needed.
	MeshCompatibilityModeAuto MeshCompatibilityMode = "auto"
)

// DomainTemplateValues are the available properties people can choose from
// in their Route's "DomainTemplate" golang template sting.
// We could add more over time - e.g. RevisionName if we thought that
// might be of interest to people.
type DomainTemplateValues struct {
	Name        string
	Namespace   string
	Domain      string
	Annotations map[string]string
	Labels      map[string]string
}

// TagTemplateValues are the available properties people can choose from
// in their Route's "TagTemplate" golang template sting.
type TagTemplateValues struct {
	Name string
	Tag  string
}

// Config contains the networking configuration defined in the
// network config map.
type Config struct {
	// DefaultIngressClass specifies the default Ingress class.
	DefaultIngressClass string

	// DomainTemplate is the golang text template to use to generate the
	// Route's domain (host) for the Service.
	DomainTemplate string

	// TagTemplate is the golang text template to use to generate the
	// Route's tag hostnames.
	TagTemplate string

	// AutoTLS specifies if auto-TLS is enabled or not.
	AutoTLS bool

	// HTTPProtocol specifics the behavior of HTTP endpoint of Knative
	// ingress.
	HTTPProtocol HTTPProtocol

	// DefaultCertificateClass specifies the default Certificate class.
	DefaultCertificateClass string

	// NamespaceWildcardCertSelector specifies the set of namespaces which should
	// have wildcard certificates provisioned for the Knative Services within.
	// Defaults to empty (selecting no namespaces). If set to an exclude rule like:
	// ```
	//   matchExpressions:
	//     key: "kubernetes.io/metadata.name"
	//     operator: "NotIn"
	//     values: ["kube-system"]
	// ```
	// This can be used to enbale wildcard certs in all non-system namespaces
	NamespaceWildcardCertSelector *metav1.LabelSelector

	// RolloutDurationSecs specifies the default duration for the rollout.
	RolloutDurationSecs int

	// AutocreateClusterDomainClaims specifies whether cluster-wide DomainClaims
	// should be automatically created (and deleted) as needed when a
	// DomainMapping is reconciled. If this is false, the
	// cluster administrator is responsible for pre-creating ClusterDomainClaims
	// and delegating them to namespaces via their spec.Namespace field.
	AutocreateClusterDomainClaims bool

	// EnableMeshPodAddressability specifies whether networking plugins will add
	// additional information to deployed applications to make their pods directl
	// accessible via their IPs even if mesh is enabled and thus direct-addressability
	// is usually not possible.
	// Consumers like Knative Serving can use this setting to adjust their behavior
	// accordingly, i.e. to drop fallback solutions for non-pod-addressable systems.
	EnableMeshPodAddressability bool

	// MeshCompatibilityMode specifies whether consumers, such as Knative Serving, should
	// attempt to directly contact pods via their IP (most efficient), or should
	// use the Cluster IP (less efficient, but needed if mesh is enabled unless
	// the EnableMeshPodAddressability option is enabled).
	MeshCompatibilityMode MeshCompatibilityMode

	// DefaultExternalScheme defines the scheme used in external URLs if AutoTLS is
	// not enabled. Defaults to "http".
	DefaultExternalScheme string

	// Deprecated - replaced with InternalDataplaneTrust and InternalControlplaneTrust
	// InternalEncryption specifies whether internal traffic is encrypted or not.
	InternalEncryption bool

	// DataplaneTrust specifies the level of trust used for date plane.
	DataplaneTrust Trust

	// ControlplaneTrust specifies the level of trust used for control plane.
	ControlplaneTrust Trust
}

func defaultConfig() *Config {
	return &Config{
		DefaultIngressClass:           IstioIngressClassName,
		DefaultCertificateClass:       CertManagerCertificateClassName,
		DomainTemplate:                DefaultDomainTemplate,
		TagTemplate:                   DefaultTagTemplate,
		AutoTLS:                       false,
		NamespaceWildcardCertSelector: nil,
		HTTPProtocol:                  HTTPEnabled,
		AutocreateClusterDomainClaims: false,
		DefaultExternalScheme:         "http",
		MeshCompatibilityMode:         MeshCompatibilityModeAuto,
		InternalEncryption:            false,
		DataplaneTrust:                TrustDisabled,
		ControlplaneTrust:             TrustDisabled,
	}
}

// NewConfigFromMap creates a Config from the supplied data.
func NewConfigFromMap(data map[string]string) (*Config, error) {
	nc := defaultConfig()

	if err := cm.Parse(data,
		// Legacy keys
		cm.AsString("ingress.class", &nc.DefaultIngressClass),
		cm.AsString("certificate.class", &nc.DefaultCertificateClass),
		cm.AsString("domainTemplate", &nc.DomainTemplate),
		cm.AsString("tagTemplate", &nc.TagTemplate),
		cm.AsInt("rolloutDuration", &nc.RolloutDurationSecs),
		cm.AsBool("autocreateClusterDomainClaims", &nc.AutocreateClusterDomainClaims),
		cm.AsString("defaultExternalScheme", &nc.DefaultExternalScheme),

		// New key takes precedence.
		cm.AsString(DefaultIngressClassKey, &nc.DefaultIngressClass),
		cm.AsString(DefaultCertificateClassKey, &nc.DefaultCertificateClass),
		cm.AsString(DomainTemplateKey, &nc.DomainTemplate),
		cm.AsString(TagTemplateKey, &nc.TagTemplate),
		cm.AsInt(RolloutDurationKey, &nc.RolloutDurationSecs),
		cm.AsBool(AutocreateClusterDomainClaimsKey, &nc.AutocreateClusterDomainClaims),
		cm.AsBool(EnableMeshPodAddressabilityKey, &nc.EnableMeshPodAddressability),
		cm.AsString(DefaultExternalSchemeKey, &nc.DefaultExternalScheme),
		cm.AsBool(InternalEncryptionKey, &nc.InternalEncryption),
		asMode(MeshCompatibilityModeKey, &nc.MeshCompatibilityMode),
		asLabelSelector(NamespaceWildcardCertSelectorKey, &nc.NamespaceWildcardCertSelector),
	); err != nil {
		return nil, err
	}

	if nc.RolloutDurationSecs < 0 {
		return nil, fmt.Errorf("%s must be a positive integer, but was %d", RolloutDurationKey, nc.RolloutDurationSecs)
	}
	// Verify domain-template and add to the cache.
	t, err := template.New("domain-template").Parse(nc.DomainTemplate)
	if err != nil {
		return nil, err
	}
	if err := checkDomainTemplate(t); err != nil {
		return nil, err
	}
	templateCache.Add(nc.DomainTemplate, t)

	// Verify tag-template and add to the cache.
	t, err = template.New("tag-template").Parse(nc.TagTemplate)
	if err != nil {
		return nil, err
	}
	if err := checkTagTemplate(t); err != nil {
		return nil, err
	}
	templateCache.Add(nc.TagTemplate, t)

	if val, ok := data["autoTLS"]; ok {
		nc.AutoTLS = strings.EqualFold(val, "enabled")
	}
	if val, ok := data[AutoTLSKey]; ok {
		nc.AutoTLS = strings.EqualFold(val, "enabled")
	}

	var httpProtocol string
	if val, ok := data["httpProtocol"]; ok {
		httpProtocol = val
	}
	if val, ok := data[HTTPProtocolKey]; ok {
		httpProtocol = val
	}

	switch strings.ToLower(httpProtocol) {
	case "", string(HTTPEnabled):
		// If HTTPProtocol is not set in the config-network, default is already
		// set to HTTPEnabled.
	case string(HTTPDisabled):
		nc.HTTPProtocol = HTTPDisabled
	case string(HTTPRedirected):
		nc.HTTPProtocol = HTTPRedirected
	default:
		return nil, fmt.Errorf("httpProtocol %s in config-network ConfigMap is not supported", data[HTTPProtocolKey])
	}

	switch strings.ToLower(data[DataplaneTrustKey]) {
	case "", string(TrustDisabled):
		// If DataplaneTrus is not set in the config-network, default is already
		// set to TrustDisabled.
		if nc.InternalEncryption {
			// Backward compatibility
			nc.DataplaneTrust = TrustMinimal
		}
	case string(TrustMinimal):
		nc.DataplaneTrust = TrustMinimal
	case string(TrustEnabled):
		nc.DataplaneTrust = TrustEnabled
	case string(TrustMutual):
		nc.DataplaneTrust = TrustMutual
	case string(TrustIdentity):
		nc.DataplaneTrust = TrustIdentity
	default:
		return nil, fmt.Errorf("DataplaneTrust %q in config-network ConfigMap is not supported", data[DataplaneTrustKey])
	}

	switch strings.ToLower(data[ControlplaneTrustKey]) {
	case "", string(TrustDisabled):
		// If ControlplaneTrust is not set in the config-network, default is already
		// set to TrustDisabled.
	case string(TrustEnabled):
		nc.ControlplaneTrust = TrustEnabled
	case string(TrustMutual):
		nc.ControlplaneTrust = TrustMutual
	default:
		return nil, fmt.Errorf("ControlplaneTrust %q in config-network ConfigMap is not supported", data[ControlplaneTrustKey])
	}

	return nc, nil
}

// GetDomainTemplate returns the golang Template from the config map
// or panics (the value is validated during CM validation and at
// this point guaranteed to be parseable).
func (c *Config) GetDomainTemplate() *template.Template {
	if tt, ok := templateCache.Get(c.DomainTemplate); ok {
		return tt.(*template.Template)
	}
	// Should not really happen outside of route/ingress unit tests.
	nt := template.Must(template.New("domain-template").Parse(
		c.DomainTemplate))
	templateCache.Add(c.DomainTemplate, nt)
	return nt
}

func checkDomainTemplate(t *template.Template) error {
	// To a test run of applying the template, and see if the
	// result is a valid URL.
	data := DomainTemplateValues{
		Name:        "foo",
		Namespace:   "bar",
		Domain:      "baz.com",
		Annotations: nil,
		Labels:      nil,
	}
	buf := bytes.Buffer{}
	if err := t.Execute(&buf, data); err != nil {
		return err
	}
	u, err := url.Parse("https://" + buf.String())
	if err != nil {
		return err
	}

	// TODO(mattmoor): Consider validating things like changing
	// Name / Namespace changes the resulting hostname.
	if u.Hostname() == "" {
		return errors.New("empty hostname")
	}
	if u.RequestURI() != "/" {
		return fmt.Errorf("domain template has url path: %s", u.RequestURI())
	}

	return nil
}

// GetTagTemplate returns the go template for the route tag.
func (c *Config) GetTagTemplate() *template.Template {
	if tt, ok := templateCache.Get(c.TagTemplate); ok {
		return tt.(*template.Template)
	}
	// Should not really happen outside of route/ingress unit tests.
	nt := template.Must(template.New("tag-template").Parse(
		c.TagTemplate))
	templateCache.Add(c.TagTemplate, nt)
	return nt
}

func checkTagTemplate(t *template.Template) error {
	// To a test run of applying the template, and see if we
	// produce a result without error.
	data := TagTemplateValues{
		Name: "foo",
		Tag:  "v2",
	}
	return t.Execute(io.Discard, data)
}

// asLabelSelector returns a LabelSelector extracted from a given configmap key.
func asLabelSelector(key string, target **metav1.LabelSelector) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			if len(raw) > 0 {
				var selector *metav1.LabelSelector
				if err := yaml.Unmarshal([]byte(raw), &selector); err != nil {
					return err
				}
				*target = selector
			}
		}
		return nil
	}
}

// asMode parses the value at key as a MeshCompatibilityMode into the target, if it exists.
func asMode(key string, target *MeshCompatibilityMode) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			for _, flag := range []MeshCompatibilityMode{MeshCompatibilityModeEnabled, MeshCompatibilityModeDisabled, MeshCompatibilityModeAuto} {
				if strings.EqualFold(raw, string(flag)) {
					*target = flag
					return nil
				}
			}
		}
		return nil
	}
}
