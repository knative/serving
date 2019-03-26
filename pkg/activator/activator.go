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

	"github.com/knative/serving/pkg/apis/networking"
)

const (
	// Name is the name of the component.
	Name = "activator"
	// K8sServiceName is the name of the activator service
	K8sServiceName = "activator-service"
	// RevisionHeaderName is the header key for revision name
	RevisionHeaderName string = "knative-serving-revision"
	// RevisionHeaderNamespace is the header key for revision's namespace
	RevisionHeaderNamespace string = "knative-serving-namespace"

	// ServicePortHTTP1 is the port number for activating HTTP1 revisions
	ServicePortHTTP1 int32 = 80
	// ServicePortH2C is the port number for activating H2C revisions
	ServicePortH2C int32 = 81
)

// RevisionID is the combination of namespace and service name
type RevisionID struct {
	Namespace string
	Name      string
}

func (rev RevisionID) String() string {
	return fmt.Sprintf("%s/%s", rev.Namespace, rev.Name)
}

// ServicePort returns the activator service port for the given app level protocol.
// Default is `ServicePortHTTP1`.
func ServicePort(protocol networking.ProtocolType) int32 {
	if protocol == networking.ProtocolH2C {
		return ServicePortH2C
	}
	return ServicePortHTTP1
}
