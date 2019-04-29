/*
Copyright 2019 The Knative Authors

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
	"github.com/ghodss/yaml"

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	solverConfigKey = "solverConfig"
	issuerRefKey    = "issuerRef"

	// CertManagerConfigName is the name of the configmap containing all
	// configuration related to Cert-Manager.
	CertManagerConfigName = "config-certmanager"
)

// CertManagerConfig contains Cert-Manager related configuration defined in the
// `config-certmanager` config map.
type CertManagerConfig struct {
	SolverConfig *certmanagerv1alpha1.SolverConfig
	IssuerRef    *certmanagerv1alpha1.ObjectReference
}

// NewCertManagerConfigFromConfigMap creates an CertManagerConfig from the supplied ConfigMap
func NewCertManagerConfigFromConfigMap(configMap *corev1.ConfigMap) (*CertManagerConfig, error) {
	// TODO(zhiminx): do we need to provide the default values here?

	config := &CertManagerConfig{
		SolverConfig: &certmanagerv1alpha1.SolverConfig{},
		IssuerRef:    &certmanagerv1alpha1.ObjectReference{},
	}

	if v, ok := configMap.Data[solverConfigKey]; ok {
		if err := yaml.Unmarshal([]byte(v), config.SolverConfig); err != nil {
			return nil, err
		}
	}

	if v, ok := configMap.Data[issuerRefKey]; ok {
		if err := yaml.Unmarshal([]byte(v), config.IssuerRef); err != nil {
			return nil, err
		}
	}
	return config, nil
}
