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

package configmap

import (
	corev1 "k8s.io/api/core/v1"
	o11yconfigmap "knative.dev/pkg/observability/configmap"
	"knative.dev/serving/pkg/observability"
)

func Name() string {
	return o11yconfigmap.Name()
}

func Parse(c *corev1.ConfigMap) (*observability.Config, error) {
	return observability.NewFromMap(c.Data)
}
