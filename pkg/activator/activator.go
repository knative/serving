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

import (
	"fmt"
)

const (
	// Name is the name of the component.
	Name = "activator"
	// K8sServiceName is the name of the activator Kubernetes service.
	K8sServiceName = "activator-service"
	// RevisionHeaderName is the header key for revision name.
	RevisionHeaderName = "Knative-Serving-Revision"
	// RevisionHeaderNamespace is the header key for revision's namespace.
	RevisionHeaderNamespace = "Knative-Serving-Namespace"
)

// RevisionID is the combination of namespace and revision name
type RevisionID struct {
	Namespace string
	Name      string
}

// String returns the namespaced name of the RevisionID.
func (rev RevisionID) String() string {
	return fmt.Sprintf("%s/%s", rev.Namespace, rev.Name)
}
