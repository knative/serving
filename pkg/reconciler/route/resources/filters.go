/*
Copyright 2019 The Knative Authors

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

package resources

import (
	corev1 "k8s.io/api/core/v1"
	network "knative.dev/networking/pkg"
	"knative.dev/serving/pkg/apis/serving"
)

// IsClusterLocalService returns whether a service is cluster local.
func IsClusterLocalService(svc *corev1.Service) bool {
	return svc.GetLabels()[network.VisibilityLabelKey] == serving.VisibilityClusterLocal ||
		svc.GetLabels()[serving.VisibilityLabelKeyObsolete] == serving.VisibilityClusterLocal
}
