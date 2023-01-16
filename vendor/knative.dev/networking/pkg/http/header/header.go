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

package header

import (
	"net/http"
	"strings"
)

// HashKey & Values
const (
	// HashKey is the name of an internal header that Ingress controller
	// uses to find out which version of the networking config is deployed.
	HashKey = "K-Network-Hash"

	// HashValueOverride is the value that must appear in the HashHeaderKey
	// header in order for our network hash to be injected.
	HashValueOverride = "override"
)

// ProbeKey & Values
const (
	// ProbeKey is the name of a header that can be added to
	// requests to probe the knative networking layer.  Requests
	// with this header will not be passed to the user container or
	// included in request metrics.
	ProbeKey = "K-Network-Probe"

	// ProbeValue is the value used in 'K-Network-Probe'
	ProbeValue = "probe"
)

const (
	// ProxyKey is the name of an internal header that activator
	// uses to mark requests going through it.
	ProxyKey = "K-Proxy-Request"

	// OriginalHostKey is used to avoid Istio host based routing rules
	// in Activator.
	// The header contains the original Host value that can be rewritten
	// at the Queue proxy level back to be a host header.
	OriginalHostKey = "K-Original-Host"

	// KubeletProbeKey is the name of the header supplied by kubelet
	// probes.  Istio with mTLS rewrites probes, but their probes pass a
	// different user-agent.  So we augment the probes with this header.
	KubeletProbeKey = "K-Kubelet-Probe"

	// RouteTagKey is the name of the header entry which has a tag name as value.
	// The tag name specifies which route was expected to be chosen by Ingress.
	RouteTagKey = "Knative-Serving-Tag"

	// DefaultRouteKey is the name of the header entry
	// identifying whether a request is routed via the default route or not.
	// It has one of the string value "true" or "false".
	DefaultRouteKey = "Knative-Serving-Default-Route"

	// PassthroughLoadbalancingKey is the name of the header that directs
	// load balancers to not load balance the respective request but to
	// send it to the request's target directly.
	PassthroughLoadbalancingKey = "K-Passthrough-Lb"
)

// User Agent Key & Values
const (
	// UserAgentKey is the constant for header "User-Agent".
	UserAgentKey = "User-Agent"

	// KubeProbeUAPrefix is the user agent prefix of the probe.
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	KubeProbeUAPrefix = "kube-probe/"

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

// Accept Content Values
const (
	// ProtobufMIMEType is a content type to be used when autoscaler scrapes metrics from the QP
	ProtobufMIMEType = "application/protobuf"
)

// KnativeProbeHeader returns the value for key ProbeHeaderName in request headers.
func GetKnativeProbeValue(r *http.Request) string {
	return r.Header.Get(ProbeKey)
}

// KnativeProxyHeader returns the value for key ProxyHeaderName in request headers.
func GetKnativeProxyValue(r *http.Request) string {
	return r.Header.Get(ProxyKey)
}

// IsProbe returns true if the request is a Kubernetes probe or a Knative probe,
// i.e. non-empty ProbeHeaderName header.
func IsProbe(r *http.Request) bool {
	return IsKubeletProbe(r) || GetKnativeProbeValue(r) != ""
}

// IsKubeletProbe returns true if the request is a Kubernetes probe.
func IsKubeletProbe(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("User-Agent"), KubeProbeUAPrefix) ||
		r.Header.Get(KubeletProbeKey) != ""
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
	if r.Header.Get(OriginalHostKey) == "" {
		r.Header.Set(OriginalHostKey, h)
	}
}

// RewriteHostOut undoes the `RewriteHostIn` action.
// RewriteHostOut checks if network.OriginalHostHeader was set and if it was,
// then uses that as the r.Host (which takes priority over Request.Header["Host"]).
// If the request did not have the OriginalHostHeader header set, the request is untouched.
func RewriteHostOut(r *http.Request) {
	if ohh := r.Header.Get(OriginalHostKey); ohh != "" {
		r.Host = ohh
		r.Header.Del("Host")
		r.Header.Del(OriginalHostKey)
	}
}
