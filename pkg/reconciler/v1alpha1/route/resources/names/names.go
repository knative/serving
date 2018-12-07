/*
Copyright 2018 The Knative Authors

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

package names

import (
	"strings"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
)

func K8sService(route *v1alpha1.Route) string {
	return route.Name
}

func K8sServiceFullname(route *v1alpha1.Route) string {
	return reconciler.GetK8sServiceFullname(K8sService(route), route.Namespace)
}

func prefix(s string, l int) string {
	if l > len(s) {
		return s
	}
	return string(s[0:l])
}

// ClusterIngressName returns a name that is based on the Route's
// name, namespace and UID.  The UID is the most important part, because
// it ensures the uniqueness of the name.  The Route name and namespace
// is added for human readability.
func ClusterIngressName(route *v1alpha1.Route) string {
	// Kubernetes object names cannot exceed 253 characters.
	totalLen := 253
	n, ns, uuid := route.Name, route.Namespace, string(route.ObjectMeta.UID)
	sep := "-"
	l := totalLen - len(uuid) - len(n) - 2
	if l > 0 {
		// Three-parts name: name-prefix(ns)-uuid.
		name := strings.Join([]string{n, prefix(ns, l), uuid}, sep)
		return name
	}
	l = totalLen - len(uuid) - 1
	if l > 0 {
		// Two-parts name: prefix(name)-uuid.
		return strings.Join([]string{prefix(n, l), uuid}, sep)
	}
	// UUID are only 36 characters long, so this should never happen.
	// However we leave it here to be safe when UUID may be longer
	// than 253 characters.  Taking the first 253 characters should be
	// very unique here.
	return prefix(uuid, totalLen)
}
