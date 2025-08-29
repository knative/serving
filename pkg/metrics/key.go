/*
Copyright 2025 The Knative Authors

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

package metrics

import (
	"knative.dev/pkg/observability/attributekey"
)

var (
	ServiceNameKey       = attributekey.String("kn.service.name")
	ConfigurationNameKey = attributekey.String("kn.configuration.name")
	RevisionNameKey      = attributekey.String("kn.revision.name")
	RouteNameKey         = attributekey.String("kn.route.name")
	RouteTagNameKey      = attributekey.String("kn.route.tag")
	K8sNamespaceKey      = attributekey.String("k8s.namespace.name")
	K8sPodIPKey          = attributekey.String("k8s.pod.ip")
)
