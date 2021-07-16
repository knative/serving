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

package pkg

import (
	"knative.dev/networking/pkg/config"
	"knative.dev/networking/pkg/header"
	"knative.dev/networking/pkg/k8s"
	"knative.dev/networking/pkg/k8s/label"
	"knative.dev/networking/pkg/mesh"
	"knative.dev/networking/pkg/prober/handler"
)

var (
	// ProbePath is the name of a path that activator, autoscaler and
	// prober(used by KIngress generally) use for health check.
	ProbePath = header.ProbePath

	// ProbeHeaderName is the name of a header that can be added to
	// requests to probe the knative networking layer.  Requests
	// with this header will not be passed to the user container or
	// included in request metrics.
	ProbeHeaderName = header.ProbeKey

	// ProxyHeaderName is the name of an internal header that activator
	// uses to mark requests going through it.
	ProxyHeaderName = header.ProxyKey

	// HashHeaderName is the name of an internal header that Ingress controller
	// uses to find out which version of the networking config is deployed.
	HashHeaderName = header.HashKey

	// HashHeaderValue is the value that must appear in the HashHeaderName
	// header in order for our network hash to be injected.
	HashHeaderValue = header.HashValue

	// OriginalHostHeader is used to avoid Istio host based routing rules
	// in Activator.
	// The header contains the original Host value that can be rewritten
	// at the Queue proxy level back to be a host header.
	OriginalHostHeader = header.OriginalHostKey

	// ConfigName is the name of the configmap containing all
	// customizations for networking features.
	ConfigName = config.ConfigName

	// DefaultIngressClassKey is the name of the configuration entry
	// that specifies the default Ingress.
	DefaultIngressClassKey = config.DefaultIngressClassKey

	// DefaultCertificateClassKey is the name of the configuration entry
	// that specifies the default Certificate.
	DefaultCertificateClassKey = config.DefaultCertificateClassKey

	// IstioIngressClassName value for specifying knative's Istio
	// Ingress reconciler.
	IstioIngressClassName = config.IstioIngressClassName

	// CertManagerCertificateClassName value for specifying Knative's Cert-Manager
	// Certificate reconciler.
	CertManagerCertificateClassName = config.IstioIngressClassName

	// DomainTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// Knative service's DNS name.
	DomainTemplateKey = config.DomainTemplateKey

	// TagTemplateKey is the name of the configuration entry that
	// specifies the golang template string to use to construct the
	// hostname for a Route's tag.
	TagTemplateKey = config.TagTemplateKey

	// RolloutDurationKey is the name of the configuration entry
	// that specifies the default duration of the configuration rollout.
	RolloutDurationKey = config.RolloutDurationKey

	// KubeProbeUAPrefix is the user agent prefix of the probe.
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	KubeProbeUAPrefix = header.KubeProbeUAPrefix

	// KubeletProbeHeaderName is the name of the header supplied by kubelet
	// probes.  Istio with mTLS rewrites probes, but their probes pass a
	// different user-agent.  So we augment the probes with this header.
	KubeletProbeHeaderName = header.KubeletProbeKey

	// DefaultDomainTemplate is the default golang template to use when
	// constructing the Knative Route's Domain(host)
	DefaultDomainTemplate = config.DefaultDomainTemplate

	// DefaultTagTemplate is the default golang template to use when
	// constructing the Knative Route's tag names.
	DefaultTagTemplate = config.DefaultTagTemplate

	// AutocreateClusterDomainClaimsKey is the key for the
	// AutocreateClusterDomainClaims property.
	AutocreateClusterDomainClaimsKey = config.AutocreateClusterDomainClaimsKey

	// AutoTLSKey is the name of the configuration entry
	// that specifies enabling auto-TLS or not.
	AutoTLSKey = config.AutoTLSKey

	// HTTPProtocolKey is the name of the configuration entry that
	// specifies the HTTP endpoint behavior of Knative ingress.
	HTTPProtocolKey = config.HTTPProtocolKey

	// UserAgentKey is the constant for header "User-Agent".
	UserAgentKey = header.UserAgentKey

	// ActivatorUserAgent is the user-agent header value set in probe requests sent
	// from activator.
	ActivatorUserAgent = header.ActivatorUserAgent

	// QueueProxyUserAgent is the user-agent header value set in probe requests sent
	// from queue-proxy.
	QueueProxyUserAgent = header.QueueProxyUserAgent

	// IngressReadinessUserAgent is the user-agent header value
	// set in probe requests for Ingress status.
	IngressReadinessUserAgent = header.IngressReadinessUserAgent

	// AutoscalingUserAgent is the user-agent header value set in probe
	// requests sent by autoscaling implementations.
	AutoscalingUserAgent = header.AutoscalingUserAgent

	// TagHeaderName is the name of the header entry which has a tag name as value.
	// The tag name specifies which route was expected to be chosen by Ingress.
	TagHeaderName = header.TagKey

	// DefaultRouteHeaderName is the name of the header entry
	// identifying whether a request is routed via the default route or not.
	// It has one of the string value "true" or "false".
	DefaultRouteHeaderName = header.DefaultRouteKey

	// TagHeaderBasedRoutingKey is the name of the configuration entry
	// that specifies enabling tag header based routing or not.
	TagHeaderBasedRoutingKey = config.TagHeaderBasedRoutingKey

	// ProtoAcceptContent is the content type to be used when autoscaler scrapes metrics from the QP
	ProtoAcceptContent = header.ProtoAcceptContent

	// FlushInterval controls the time when we flush the connection in the
	// reverse proxies (Activator, QP).
	// NB: having it equal to 0 is a problem for streaming requests
	// since the data won't be transferred in chunks less than 4kb, if the
	// reverse proxy fails to detect streaming (gRPC, e.g.).
	FlushInterval = header.FlushInterval

	// VisibilityLabelKey is the label to indicate visibility of Route
	// and KServices.  It can be an annotation too but since users are
	// already using labels for domain, it probably best to keep this
	// consistent.
	VisibilityLabelKey = label.VisibilityKey

	// PassthroughLoadbalancingHeaderName is the name of the header that directs
	// load balancers to not load balance the respective request but to
	// send it to the request's target directly.
	PassthroughLoadbalancingHeaderName = header.PassthroughLoadbalancingKey

	// EnableMeshPodAddressabilityKey is the config for enabling pod addressability in mesh.
	EnableMeshPodAddressabilityKey = config.EnableMeshPodAddressabilityKey

	// DefaultExternalSchemeKey is the config for defining the scheme of external URLs.
	DefaultExternalSchemeKey = config.DefaultExternalSchemeKey
)

type DomainTemplateValues = config.DomainTemplateValues

// TagTemplateValues are the available properties people can choose from
// in their Route's "TagTemplate" golang template sting.
type TagTemplateValues = config.TagTemplateValues

// network config map.
type Config = config.Config

// HTTPProtocol indicates a type of HTTP endpoint behavior
// that Knative ingress could take.
type HTTPProtocol = config.HTTPProtocol

var (
	// HTTPEnabled represents HTTP protocol is enabled in Knative ingress.
	HTTPEnabled = config.HTTPEnabled

	// HTTPDisabled represents HTTP protocol is disabled in Knative ingress.
	HTTPDisabled = config.HTTPDisabled

	// HTTPRedirected represents HTTP connection is redirected to HTTPS in Knative ingress.
	HTTPRedirected = config.HTTPRedirected
)

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap
var NewConfigFromConfigMap = config.NewFromConfigMap

// NewConfigFromMap creates a Config from the supplied data.
var NewConfigFromMap = config.NewFromMap

// IsKubeletProbe returns true if the request is a Kubernetes probe.
var IsKubeletProbe = header.IsKubeletProbe

// KnativeProbeHeader returns the value for key ProbeHeaderName in request headers.
var KnativeProbeHeader = header.GetKnativeProbeValue

// KnativeProxyHeader returns the value for key ProxyHeaderName in request headers.
var KnativeProxyHeader = header.GetKnativeProxyValue

// IsProbe returns true if the request is a Kubernetes probe or a Knative probe,
// i.e. non-empty ProbeHeaderName header.
var IsProbe = header.IsProbe

// RewriteHostIn removes the `Host` header from the inbound (server) request
// and replaces it with our custom header.
// This is done to avoid Istio Host based routing, see #3870.
// Queue-Proxy will execute the reverse process.
var RewriteHostIn = header.RewriteHostIn

// RewriteHostOut undoes the `RewriteHostIn` action.
// RewriteHostOut checks if network.OriginalHostHeader was set and if it was,
// then uses that as the r.Host (which takes priority over Request.Header["Host"]).
// If the request did not have the OriginalHostHeader header set, the request is untouched.
var RewriteHostOut = header.RewriteHostOut

// NameForPortNumber finds the name for a given port as defined by a Service.
var NameForPortNumber = k8s.NameForPortNumber

// PortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
var PortNumberForName = k8s.PortNumberForName

// IsPotentialMeshErrorResponse returns whether the HTTP response is compatible
// with having been caused by attempting direct connection when mesh was
// enabled. For example if we get a HTTP 404 status code it's safe to assume
// mesh is not enabled even if a probe was otherwise unsuccessful. This is
// useful to avoid falling back to ClusterIP when we see errors which are
// unrelated to mesh being enabled.
var IsPotentialMeshErrorResponse = mesh.IsPotentialMeshErrorResponse

// ProbeHeaderValue is the value used in 'K-Network-Probe'
var ProbeHeaderValue = header.ProbeValue

var NewProbeHandler = handler.New
