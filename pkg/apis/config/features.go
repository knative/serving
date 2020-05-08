/*
Copyright 2020 The Knative Authors

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

package config

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// FeaturesConfigName is the name of the ConfigMap for the features.
	FeaturesConfigName = "config-features"
)

func defaultFeaturesConfig() *Features {
	return &Features{
		EnableMultiContainer: false,
	}
}

// NewFeaturesConfigFromMap creates a Features from the supplied Map
func NewFeaturesConfigFromMap(data map[string]string) (*Features, error) {
	nc := defaultFeaturesConfig()
	nc.EnableMultiContainer = strings.EqualFold(data["enable-multi-container"], "true")
	return nc, nil
}

// NewFeaturesConfigFromConfigMap creates a Features from the supplied ConfigMap
func NewFeaturesConfigFromConfigMap(config *corev1.ConfigMap) (*Features, error) {
	return NewFeaturesConfigFromMap(config.Data)
}

// Features specifies which features are allowed by the webhook.
type Features struct {
	EnableMultiContainer bool
}
