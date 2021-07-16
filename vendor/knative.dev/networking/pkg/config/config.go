/*
Copyright 2021 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

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
	"io/ioutil"
	"net/url"
	"strings"
	"text/template"

	lru "github.com/hashicorp/golang-lru"
	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// AutocreateClusterDomainClaimsKey is the key for the
	// AutocreateClusterDomainClaims property.
	AutocreateClusterDomainClaimsKey = "autocreateClusterDomainClaims"

	// AutoTLSKey is the name of the configuration entry
	// that specifies enabling auto-TLS or not.
	AutoTLSKey = "autoTLS"

	// CertManagerCertificateClassName value for specifying Knative's Cert-Manager
	// Certificate reconciler.
	CertManagerCertificateClassName = "cert-manager.certificate.networking.knative.dev"

	// ConfigMapName is the name of the configmap containing all
	// customizations for networking features.
	ConfigName = "config-network"

	// DefaultCertificateClassKey is the name of the configuration entry
	// that specifies the default Certificate.
	DefaultCertificateClassKey = "certificate.class"

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	DefaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"

	// DefaultExternalSchemeKey is the config for defining the scheme of external URLs.
	DefaultExternalSchemeKey = "defaultExternalScheme"

	// DefaultIngressClassKey is the name of the configuration entry
	// that specifies the default Ingress.
	DefaultIngressClassKey = "ingress.class"

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	DefaultTagTemplate = "{{.Tag}}-{{.Name}}"

	// DomainTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// Knative service's DNS name.
	DomainTemplateKey = "domainTemplate"

	// EnableMeshPodAddressabilityKey is the config for enabling pod addressability in mesh.
	EnableMeshPodAddressabilityKey = "enable-mesh-pod-addressability"

	// IstioIngressClassName value for specifying knative's Istio
	// Ingress reconciler.
	IstioIngressClassName = "istio.ingress.networking.knative.dev"

	// RolloutDurationKey is the name of the configuration entry
	// that specifies the default duration of the configuration rollout.
	RolloutDurationKey = "rolloutDuration"

	// TagHeaderBasedRoutingKey is the name of the configuration entry
	// that specifies enabling tag header based routing or not.
	TagHeaderBasedRoutingKey = "tagHeaderBasedRouting"

	// TagTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// hostname for a Route's tag.
	TagTemplateKey = "tagTemplate"

	// HTTPProtocolKey is the name of the configuration entry that
	// specifies the HTTP endpoint behavior of Knative ingress.
	HTTPProtocolKey = "httpProtocol"
)

type (
	// Config contains the networking configuration defined in the
	// network config map.
	Config struct {
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

		// TagHeaderBasedRouting specifies if TagHeaderBasedRouting is enabled or not.
		TagHeaderBasedRouting bool

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

		// DefaultExternalScheme defines the scheme used in external URLs if AutoTLS is
		// not enabled. Defaults to "http".
		DefaultExternalScheme string
	}

	// DomainTemplateValues are the available properties people can choose from
	// in their Route's "DomainTemplate" golang template sting.
	// We could add more over time - e.g. RevisionName if we thought that
	// might be of interest to people.
	DomainTemplateValues struct {
		Name        string
		Namespace   string
		Domain      string
		Annotations map[string]string
		Labels      map[string]string
	}

	// HTTPProtocol indicates a type of HTTP endpoint behavior
	// that Knative ingress could take.
	HTTPProtocol string

	// TagTemplateValues are the available properties people can choose from
	// in their Route's "TagTemplate" golang template sting.
	TagTemplateValues struct {
		Name string
		Tag  string
	}
)

const (
	// HTTPEnabled represents HTTP protocol is enabled in Knative ingress.
	HTTPEnabled HTTPProtocol = "enabled"

	// HTTPDisabled represents HTTP protocol is disabled in Knative ingress.
	HTTPDisabled HTTPProtocol = "disabled"

	// HTTPRedirected represents HTTP connection is redirected to HTTPS in Knative ingress.
	HTTPRedirected HTTPProtocol = "redirected"
)

var (
	templateCache *lru.Cache
)

func init() {
	templateCache, _ = lru.New(10 * 2)
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewFromMap(configMap.Data)
}

// NewConfigFromMap creates a Config from the supplied data.
func NewFromMap(data map[string]string) (*Config, error) {
	nc := defaultConfig()

	if err := cm.Parse(data,
		// New key takes precedence.
		cm.AsString(DefaultIngressClassKey, &nc.DefaultIngressClass),
		cm.AsString(DefaultCertificateClassKey, &nc.DefaultCertificateClass),
		cm.AsString(DomainTemplateKey, &nc.DomainTemplate),
		cm.AsString(TagTemplateKey, &nc.TagTemplate),
		cm.AsInt(RolloutDurationKey, &nc.RolloutDurationSecs),
		cm.AsBool(AutocreateClusterDomainClaimsKey, &nc.AutocreateClusterDomainClaims),
		cm.AsBool(EnableMeshPodAddressabilityKey, &nc.EnableMeshPodAddressability),
		cm.AsString(DefaultExternalSchemeKey, &nc.DefaultExternalScheme),
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

	nc.AutoTLS = strings.EqualFold(data[AutoTLSKey], "enabled")
	nc.TagHeaderBasedRouting = strings.EqualFold(data[TagHeaderBasedRoutingKey], "enabled")

	switch strings.ToLower(data[HTTPProtocolKey]) {
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
	return nc, nil
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

func checkTagTemplate(t *template.Template) error {
	// To a test run of applying the template, and see if we
	// produce a result without error.
	data := TagTemplateValues{
		Name: "foo",
		Tag:  "v2",
	}
	return t.Execute(ioutil.Discard, data)
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

func defaultConfig() *Config {
	return &Config{
		DefaultIngressClass:           IstioIngressClassName,
		DefaultCertificateClass:       CertManagerCertificateClassName,
		DomainTemplate:                DefaultDomainTemplate,
		TagTemplate:                   DefaultTagTemplate,
		AutoTLS:                       false,
		HTTPProtocol:                  HTTPEnabled,
		AutocreateClusterDomainClaims: false,
		DefaultExternalScheme:         "http",
	}
}
