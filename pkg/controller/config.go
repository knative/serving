/*
Copyright 2018 Google LLC

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

package controller

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Contains controller configurations.
type Config struct {
	// The suffix of the domain used for routes.
	DomainSuffix string
}

const (
	elaNamespace    = "ela-system"
	domainSuffixKey = "domainSuffix"
)

func NewConfig(kubeClient kubernetes.Interface) (*Config, error) {
	m, err := kubeClient.CoreV1().ConfigMaps(elaNamespace).Get(GetElaConfigMapName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	domainSuffix, ok := m.Data[domainSuffixKey]
	if !ok {
		return nil, fmt.Errorf("key %q not found in ConfigMap %q/%q", domainSuffixKey, elaNamespace, GetElaConfigMapName())
	}
	return &Config{DomainSuffix: domainSuffix}, nil
}
