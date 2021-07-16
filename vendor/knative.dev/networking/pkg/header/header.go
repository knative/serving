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

package header

import (
	"net/http"
	"strings"
	"time"
)

const (
	// ProbeKey is the name of a header that can be added to
	// requests to probe the knative networking layer.  Requests
	// with this header will not be passed to the user container or
	// included in request metrics.
	ProbeKey = "K-Network-Probe"

	// ProbeValue used in 'K-Network-Probe'
	ProbeValue = "probe"

	// ProbePath is the name of a path that activator, autoscaler and
	ProbePath = "/healthz"

	// ProxyKey is the name of an internal header that activator
	// uses to mark requests going through it.
	ProxyKey = "K-Proxy-Request"

	// HashKey is the name of an internal header that Ingress controller
	// uses to find out which version of the networking config is deployed.
	HashKey = "K-Network-Hash"

	// HashValue is the value that must appear in the HashKey
	// header in order for our network hash to be injected.
	HashValue = "override"

	// OriginalHostKey is used to avoid Istio host based routing rules
	// in Activator.
	// The header contains the original Host value that can be rewritten
	// at the Queue proxy level back to be a host header.
	OriginalHostKey = "K-Original-Host"

	// KubeProbeUAPrefix is the user agent prefix of the probe.
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	KubeProbeUAPrefix = "kube-probe/"

	// KubeletProbeKey is the name of the header supplied by kubelet
	// probes.  Istio with mTLS rewrites probes, but their probes pass a
	// different user-agent.  So we augment the probes with this header.
	KubeletProbeKey = "K-Kubelet-Probe"

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

	// TagKey is the name of the header entry which has a tag name as value.
	// The tag name specifies which route was expected to be chosen by Ingress.
	TagKey = "Knative-Serving-Tag"

	// DefaultRouteKey is the name of the header entry
	// identifying whether a request is routed via the default route or not.
	// It has one of the string value "true" or "false".
	DefaultRouteKey = "Knative-Serving-Default-Route"

	// ProtoAcceptContent is the content type to be used when autoscaler scrapes metrics from the QP
	ProtoAcceptContent = "application/protobuf"

	// FlushInterval controls the time when we flush the connection in the
	// reverse proxies (Activator, QP).
	// NB: having it equal to 0 is a problem for streaming requests
	// since the data won't be transferred in chunks less than 4kb, if the
	// reverse proxy fails to detect streaming (gRPC, e.g.).
	FlushInterval = 20 * time.Millisecond

	// PassthroughLoadbalancingKey is the name of the header that directs
	// load balancers to not load balance the respective request but to
	// send it to the request's target directly.
	PassthroughLoadbalancingKey = "K-Passthrough-Lb"
)

// IsKubeletProbe returns true if the request is a Kubernetes probe.
func IsKubeletProbe(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("User-Agent"), KubeProbeUAPrefix) ||
		r.Header.Get(KubeletProbeKey) != ""
}

// KnativeProbeHeader returns the value for key ProbeKey in request headers.
func GetKnativeProbeValue(r *http.Request) string {
	return r.Header.Get(ProbeKey)
}

// KnativeProxyHeader returns the value for key ProxyKey in request headers.
func GetKnativeProxyValue(r *http.Request) string {
	return r.Header.Get(ProxyKey)
}

// IsProbe returns true if the request is a Kubernetes probe or a Knative probe,
// i.e. non-empty ProbeKey header.
func IsProbe(r *http.Request) bool {
	return IsKubeletProbe(r) || GetKnativeProbeValue(r) != ""
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
