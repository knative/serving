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

package queue

const (
	// Name is the name of the component.
	Name = "queue"

	// RequestQueueDrainPath specifies the path to wait until the proxy
	// server is shut down. Any subsequent calls to this endpoint after
	// the server has finished shutting down it will return immediately.
	// Main usage is to delay the termination of user-container until all
	// accepted requests have been processed.
	RequestQueueDrainPath = "/wait-for-drain"

	// DummyProbePath is used to expose a liveness probe on the queue-proxy
	// that is not intended to establish any sort of healthiness, but to
	// work around a bug in Istio.
	// When run with the Istio mesh, Envoy blocks traffic to any ports not
	// recognized, and has special treatment for probes, but not PreStop hooks.
	// So we use this dummy path to expose an otherwise useless probe to
	// workaround the bug.  See: #5540
	DummyProbePath = "/dummy-probe"
)
