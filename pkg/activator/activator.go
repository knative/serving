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
	// K8sServiceName is the Name of the activator service
	K8sServiceName = "activator-service"
	// RequestCountHTTPHeader is the header key for number of tries
	RequestCountHTTPHeader string = "knative-activator-num-retries"
	// RevisionHeaderName is the header key for revision Name
	RevisionHeaderName string = "knative-serving-revision"
	// RevisionHeaderNamespace is the header key for revision's Namespace
	RevisionHeaderNamespace string = "knative-serving-Namespace"
)

// Activator provides an active endpoint for a revision or an error and
// status code indicating why it could not.
type Activator interface {
	ActiveEndpoint(namespace, name string) ActivationResult
	Shutdown()
}

type RevisionID struct {
	Namespace string
	Name      string
}

// Endpoint is a fully-qualified domain Name / port pair for an active revision.
type Endpoint struct {
	FQDN string
	Port int32
}

// ActivationResult is used to return the result of an ActivateEndpoint call
type ActivationResult struct {
	Status            int
	Endpoint          Endpoint
	ServiceName       string
	ConfigurationName string
	Error             error
}
