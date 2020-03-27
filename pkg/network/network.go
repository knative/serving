/*
Copyright 2018 The Knative Authors

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

package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"text/template"

	lru "github.com/hashicorp/golang-lru"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/logging"
)

const (
	// ProbeHeaderName is the name of a header that can be added to
	// requests to probe the knative networking layer.  Requests
	// with this header will not be passed to the user container or
	// included in request metrics.
	ProbeHeaderName = "K-Network-Probe"

	// ProxyHeaderName is the name of an internal header that activator
	// uses to mark requests going through it.
	ProxyHeaderName = "K-Proxy-Request"

	// HashHeaderName is the name of an internal header that Ingress controller
	// uses to find out which version of the networking config is deployed.
	HashHeaderName = "K-Network-Hash"

	// OriginalHostHeader is used to avoid Istio host based routing rules
	// in Activator.
	// The header contains the original Host value that can be rewritten
	// at the Queue proxy level back to be a host header.
	OriginalHostHeader = "K-Original-Host"

	// ConfigName is the name of the configmap containing all
	// customizations for networking features.
	ConfigName = "config-network"

	// IstioOutboundIPRangesKey is the name of the configuration entry
	// that specifies Istio outbound ip ranges.
	//
	// DEPRECATED: This will be completely removed in the future release.
	IstioOutboundIPRangesKey = "istio.sidecar.includeOutboundIPRanges"

	// DeprecatedDefaultIngressClassKey  Please use DefaultIngressClassKey instead.
	DeprecatedDefaultIngressClassKey = "clusteringress.class"

	// DefaultIngressClassKey is the name of the configuration entry
	// that specifies the default Ingress.
	DefaultIngressClassKey = "ingress.class"

	// DefaultCertificateClassKey is the name of the configuration entry
	// that specifies the default Certificate.
	DefaultCertificateClassKey = "certificate.class"

	// IstioIngressClassName value for specifying knative's Istio
	// Ingress reconciler.
	IstioIngressClassName = "istio.ingress.networking.knative.dev"

	// CertManagerCertificateClassName value for specifying Knative's Cert-Manager
	// Certificate reconciler.
	CertManagerCertificateClassName = "cert-manager.certificate.networking.knative.dev"

	// DomainTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// Knative service's DNS name.
	DomainTemplateKey = "domainTemplate"

	// TagTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// hostname for a Route's tag.
	TagTemplateKey = "tagTemplate"

	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	KubeProbeUAPrefix = "kube-probe/"

	// Istio with mTLS rewrites probes, but their probes pass a different
	// user-agent.  So we augment the probes with this header.
	KubeletProbeHeaderName = "K-Kubelet-Probe"

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	DefaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	DefaultTagTemplate = "{{.Tag}}-{{.Name}}"

	// AutoTLSKey is the name of the configuration entry
	// that specifies enabling auto-TLS or not.
	AutoTLSKey = "autoTLS"

	// HTTPProtocolKey is the name of the configuration entry that
	// specifies the HTTP endpoint behavior of Knative ingress.
	HTTPProtocolKey = "httpProtocol"

	// UserAgentKey is the constant for header "User-Agent".
	UserAgentKey = "User-Agent"

	// ActivatorUserAgent is the user-agent header value set in probe requests sent
	// from activator.
	ActivatorUserAgent = "Knative-Activator-Probe"

	// QueueProxyUserAgent is the user-agent header value set in probe requests sent
	// from queue-proxy.
	QueueProxyUserAgent = "Knative-Queue-Proxy-Probe"

	// IngressReadinessUserAgent is the user-agent header value
	// set in probe requests for Ingress status.
	IngressReadinessUserAgent = "Knative-Ingress-Probe"

	// AutoscalingUserAgent is the user-agent header value set in probe
	// requests sent by autoscaling implementations.
	AutoscalingUserAgent = "Knative-Autoscaling-Probe"
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
}

// TagTemplateValues are the available properties people can choose from
// in their Route's "TagTemplate" golang template sting.
type TagTemplateValues struct {
	Name string
	Tag  string
}

var (
	templateCache         *lru.Cache
	defaultDomainTemplate = template.Must(template.New("domain-template").Parse(DefaultDomainTemplate))
	defaultTagTemplate    = template.Must(template.New("tag-template").Parse(DefaultTagTemplate))
)

func init() {
	// The only failure is due to negative size.
	// Store ~10 latest templates per template type.
	templateCache, _ = lru.New(10 * 2)
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
}

// HTTPProtocol indicates a type of HTTP endpoint behavior
// that Knative ingress could take.
type HTTPProtocol string

const (
	// HTTPEnabled represents HTTP proocol is enabled in Knative ingress.
	HTTPEnabled HTTPProtocol = "enabled"

	// HTTPDisabled represents HTTP protocol is disabled in Knative ingress.
	HTTPDisabled HTTPProtocol = "disabled"

	// HTTPRedirected represents HTTP connection is redirected to HTTPS in Knative ingress.
	HTTPRedirected HTTPProtocol = "redirected"
)

func defaultConfig() *Config {
	return &Config{
		DefaultIngressClass:     IstioIngressClassName,
		DefaultCertificateClass: CertManagerCertificateClassName,
		DomainTemplate:          DefaultDomainTemplate,
		TagTemplate:             DefaultTagTemplate,
		AutoTLS:                 false,
		HTTPProtocol:            HTTPEnabled,
	}
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}

// NewConfogFromMap creates a Config from the supplied data.
func NewConfigFromMap(data map[string]string) (*Config, error) {
	nc := defaultConfig()
	if _, ok := data[IstioOutboundIPRangesKey]; ok {
		// TODO(0.15): Until the next version is released, the validation check is
		// enabled to notify users who configure this value.
		logger := logging.FromContext(context.Background()).Named("config-network")
		logger.Warnf("%q is deprecated as outbound network access is enabled by default now. Remove it from config-network", IstioOutboundIPRangesKey)
	}

	if ingressClass, ok := data[DefaultIngressClassKey]; ok {
		nc.DefaultIngressClass = ingressClass
	} else if ingressClass, ok := data[DeprecatedDefaultIngressClassKey]; ok {
		nc.DefaultIngressClass = ingressClass
	}

	if certClass, ok := data[DefaultCertificateClassKey]; ok {
		nc.DefaultCertificateClass = certClass
	}

	// Blank DomainTemplate makes no sense so use our default
	if dt, ok := data[DomainTemplateKey]; ok {
		t, err := template.New("domain-template").Parse(dt)
		if err != nil {
			return nil, err
		}
		if err := checkDomainTemplate(t); err != nil {
			return nil, err
		}
		templateCache.Add(dt, t)
		nc.DomainTemplate = dt
	} else {
		// Make sure default template is in the cache.
		templateCache.Add(DefaultDomainTemplate, defaultDomainTemplate)
	}

	// Blank TagTemplate makes no sense so use our default
	if tt, ok := data[TagTemplateKey]; ok {
		t, err := template.New("tag-template").Parse(tt)
		if err != nil {
			return nil, err
		}
		if err := checkTagTemplate(t); err != nil {
			return nil, err
		}
		templateCache.Add(tt, t)
		nc.TagTemplate = tt
	} else {
		// Make sure default template is in the cache.
		templateCache.Add(DefaultTagTemplate, defaultTagTemplate)
	}

	nc.AutoTLS = strings.EqualFold(data[AutoTLSKey], "enabled")

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

// GetDomainTemplate returns the golang Template from the config map
// or panics (the value is validated during CM validation and at
// this point guaranteed to be parseable).
func (c *Config) GetDomainTemplate() *template.Template {
	if tt, ok := templateCache.Get(c.DomainTemplate); ok {
		return tt.(*template.Template)
	} else {
		// Should not really happen outside of route/ingress unit tests.
		nt := template.Must(template.New("domain-template").Parse(
			c.DomainTemplate))
		templateCache.Add(c.DomainTemplate, nt)
		return nt
	}
}

func checkDomainTemplate(t *template.Template) error {
	// To a test run of applying the template, and see if the
	// result is a valid URL.
	data := DomainTemplateValues{
		Name:        "foo",
		Namespace:   "bar",
		Domain:      "baz.com",
		Annotations: nil,
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

func (c *Config) GetTagTemplate() *template.Template {
	if tt, ok := templateCache.Get(c.TagTemplate); ok {
		return tt.(*template.Template)
	} else {
		// Should not really happen outside of route/ingress unit tests.
		nt := template.Must(template.New("tag-template").Parse(
			c.TagTemplate))
		templateCache.Add(c.TagTemplate, nt)
		return nt
	}
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

// IsKubeletProbe returns true if the request is a Kubernetes probe.
func IsKubeletProbe(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("User-Agent"), KubeProbeUAPrefix) ||
		r.Header.Get(KubeletProbeHeaderName) != ""
}

// KnativeProbeHeader returns the value for key ProbeHeaderName in request headers.
func KnativeProbeHeader(r *http.Request) string {
	return r.Header.Get(ProbeHeaderName)
}

// KnativeProxyHeader returns the value for key ProxyHeaderName in request headers.
func KnativeProxyHeader(r *http.Request) string {
	return r.Header.Get(ProxyHeaderName)
}

// IsProbe returns true if the request is a Kubernetes probe or a Knative probe,
// i.e. non-empty ProbeHeaderName header.
func IsProbe(r *http.Request) bool {
	return IsKubeletProbe(r) || KnativeProbeHeader(r) != ""
}

// RewriteHostIn removes the `Host` header from the inbound (server) request
// and replaces it with our custom header.
// This is done to avoid Istio Host based routing, see #3870.
// Queue-Proxy will execute the reverse process.
func RewriteHostIn(r *http.Request) {
	h := r.Host
	r.Host = ""
	r.Header.Del("Host")
	// Don't overwrite an existing OriginalHostHeader.
	if r.Header.Get(OriginalHostHeader) == "" {
		r.Header.Set(OriginalHostHeader, h)
	}
}

// RewriteHostOut undoes the `RewriteHostIn` action.
// RewriteHostOut checks if network.OriginalHostHeader was set and if it was,
// then uses that as the r.Host (which takes priority over Request.Header["Host"]).
// If the request did not have the OriginalHostHeader header set, the request is untouched.
func RewriteHostOut(r *http.Request) {
	if ohh := r.Header.Get(OriginalHostHeader); ohh != "" {
		r.Host = ohh
		r.Header.Del("Host")
		r.Header.Del(OriginalHostHeader)
	}
}

// NameForPortNumber finds the name for a given port as defined by a Service.
func NameForPortNumber(svc *corev1.Service, portNumber int32) (string, error) {
	for _, port := range svc.Spec.Ports {
		if port.Port == portNumber {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("no port with number %d found", portNumber)
}

// PortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
func PortNumberForName(sub corev1.EndpointSubset, portName string) (int32, error) {
	for _, subPort := range sub.Ports {
		if subPort.Name == portName {
			return subPort.Port, nil
		}
	}
	return 0, fmt.Errorf("no port for name %q found", portName)
}
