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

package pkg

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	lru "github.com/hashicorp/golang-lru"
	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// ProbePath is the name of a path that activator, autoscaler and
	// prober(used by KIngress generally) use for health check.
	ProbePath = "/healthz"

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

	// HashHeaderValue is the value that must appear in the HashHeaderName
	// header in order for our network hash to be injected.
	HashHeaderValue = "override"

	// OriginalHostHeader is used to avoid Istio host based routing rules
	// in Activator.
	// The header contains the original Host value that can be rewritten
	// at the Queue proxy level back to be a host header.
	OriginalHostHeader = "K-Original-Host"

	// ConfigName is the name of the configmap containing all
	// customizations for networking features.
	ConfigName = "config-network"

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

	// RolloutDurationKey is the name of the configuration entry
	// that specifies the default duration of the configuration rollout.
	RolloutDurationKey = "rolloutDuration"

	// KubeProbeUAPrefix is the user agent prefix of the probe.
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	KubeProbeUAPrefix = "kube-probe/"

	// KubeletProbeHeaderName is the name of the header supplied by kubelet
	// probes.  Istio with mTLS rewrites probes, but their probes pass a
	// different user-agent.  So we augment the probes with this header.
	KubeletProbeHeaderName = "K-Kubelet-Probe"

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	DefaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	DefaultTagTemplate = "{{.Tag}}-{{.Name}}"

	// AutocreateClusterDomainClaimsKey is the key for the
	// AutocreateClusterDomainClaims property.
	AutocreateClusterDomainClaimsKey = "autocreateClusterDomainClaims"

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

	// TagHeaderName is the name of the header entry which has a tag name as value.
	// The tag name specifies which route was expected to be chosen by Ingress.
	TagHeaderName = "Knative-Serving-Tag"

	// DefaultRouteHeaderName is the name of the header entry
	// identifying whether a request is routed via the default route or not.
	// It has one of the string value "true" or "false".
	DefaultRouteHeaderName = "Knative-Serving-Default-Route"

	// TagHeaderBasedRoutingKey is the name of the configuration entry
	// that specifies enabling tag header based routing or not.
	TagHeaderBasedRoutingKey = "tagHeaderBasedRouting"

	// ProtoAcceptContent is the content type to be used when autoscaler scrapes metrics from the QP
	ProtoAcceptContent = "application/protobuf"

	// FlushInterval controls the time when we flush the connection in the
	// reverse proxies (Activator, QP).
	// NB: having it equal to 0 is a problem for streaming requests
	// since the data won't be transferred in chunks less than 4kb, if the
	// reverse proxy fails to detect streaming (gRPC, e.g.).
	FlushInterval = 20 * time.Millisecond

	// VisibilityLabelKey is the label to indicate visibility of Route
	// and KServices.  It can be an annotation too but since users are
	// already using labels for domain, it probably best to keep this
	// consistent.
	VisibilityLabelKey = "networking.knative.dev/visibility"

	// PassthroughLoadbalancingHeaderName is the name of the header that directs
	// load balancers to not load balance the respective request but to
	// send it to the request's target directly.
	PassthroughLoadbalancingHeaderName = "K-Passthrough-Lb"

	// EnableMeshPodAddressabilityKey is the config for enabling pod addressability in mesh.
	EnableMeshPodAddressabilityKey = "enable-mesh-pod-addressability"

	// DefaultExternalSchemeKey is the config for defining the scheme of external URLs.
	DefaultExternalSchemeKey = "defaultExternalScheme"
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

var (
	templateCache *lru.Cache

	// Verify the default templates are valid.
	_ = template.Must(template.New("domain-template").Parse(DefaultDomainTemplate))
	_ = template.Must(template.New("tag-template").Parse(DefaultTagTemplate))
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

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(configMap.Data)
}

// NewConfigFromMap creates a Config from the supplied data.
func NewConfigFromMap(data map[string]string) (*Config, error) {
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

// IsPotentialMeshErrorResponse returns whether the HTTP response is compatible
// with having been caused by attempting direct connection when mesh was
// enabled. For example if we get a HTTP 404 status code it's safe to assume
// mesh is not enabled even if a probe was otherwise unsuccessful. This is
// useful to avoid falling back to ClusterIP when we see errors which are
// unrelated to mesh being enabled.
func IsPotentialMeshErrorResponse(resp *http.Response) bool {
	return resp.StatusCode == http.StatusServiceUnavailable
}
