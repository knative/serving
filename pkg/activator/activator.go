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
package activator

const (
	// The name of the activator service.
	K8sServiceName = "activator-service"
)

// Status is an HTTP status code.
type Status int

// Activator provides an active endpoint for a revision or an error and
// status code indicating why it could not.
type Activator interface {
	ActiveEndpoint(namespace, name string) (Endpoint, Status, error)
	Shutdown()
}

type revisionID struct {
	namespace string
	name      string
}

// Endpoint is a fully-qualified domain name / port pair for an active revision.
type Endpoint struct {
	FQDN string
	Port int32
}
