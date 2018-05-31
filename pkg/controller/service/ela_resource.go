/*
Copyright 2018 Google LLC

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
package service

import (
	"github.com/knative/serving/pkg/apis/ela"
	"github.com/knative/serving/pkg/apis/ela/v1alpha1"
)

// MakeElaResourceLabels constructs the labels we will apply to Route and Configuration
// resources.
func MakeElaResourceLabels(s *v1alpha1.Service) map[string]string {
	labels := make(map[string]string, len(s.ObjectMeta.Labels)+1)
	labels[ela.ServiceLabelKey] = s.Name

	for k, v := range s.ObjectMeta.Labels {
		labels[k] = v
	}
	return labels
}
