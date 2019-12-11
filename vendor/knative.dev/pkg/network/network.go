/*
Copyright 2019 The Knative Authors

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
	"time"
)

const (
	// DefaultConnTimeout specifies a short default connection timeout
	// to avoid hitting the issue fixed in
	// https://github.com/kubernetes/kubernetes/pull/72534 but only
	// avalailable after Kubernetes 1.14.
	//
	// Our connections are usually between pods in the same cluster
	// like activator <-> queue-proxy, or even between containers
	// within the same pod queue-proxy <-> user-container, so a
	// smaller connect timeout would be justifiable.
	//
	// We should consider exposing this as a configuration.
	DefaultConnTimeout = 200 * time.Millisecond

	// UserAgentKey is the constant for header "User-Agent".
	UserAgentKey = "User-Agent"

	// ProbeHeaderName is the name of a header that can be added to
	// requests to probe the knative networking layer.  Requests
	// with this header will not be passed to the user container or
	// included in request metrics.
	ProbeHeaderName = "K-Network-Probe"
)
