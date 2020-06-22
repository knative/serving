/*
Copyright 2020 The Knative Authors

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

package leaderelection

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	kle "knative.dev/pkg/leaderelection"
)

var (
	validComponents = sets.NewString(
		"controller",
		"hpaautoscaler",
		"certcontroller",
		"istiocontroller",
		"nscontroller",
		"webhook",
	)
)

// ValidateConfig enriches the leader election config validation
// with extra validations specific to serving.
func ValidateConfig(configMap *corev1.ConfigMap) (*kle.Config, error) {
	config, err := kle.NewConfigFromMap(configMap.Data)
	if err != nil {
		return nil, err
	}

	for component := range config.EnabledComponents {
		if !validComponents.Has(component) {
			return nil, fmt.Errorf("invalid enabledComponent %q: valid values are %q", component, validComponents.List())
		}
	}

	return config, nil
}
