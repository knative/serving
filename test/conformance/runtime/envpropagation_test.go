// +build e2e

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

package runtime

import (
	"fmt"
	"testing"

	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"

	. "github.com/knative/serving/pkg/testing/v1alpha1"
)

// TestSecretsViaEnv verifies propagation of Secrets through environment variables.
func TestSecretsViaEnv(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	t.Run("env", func(t *testing.T) {
		err := fetchEnvironmentAndVerify(t, clients, WithEnv(corev1.EnvVar{
			Name: test.EnvKey,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: test.ConformanceSecret,
					},
					Key: test.EnvKey,
				},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("envFrom", func(t *testing.T) {
		err := fetchEnvironmentAndVerify(t, clients, WithEnvFrom(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: test.ConformanceSecret,
				},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
	})
}

// TestConfigsViaEnv verifies propagation of configs through environment variables.
func TestConfigsViaEnv(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	t.Run("env", func(t *testing.T) {
		err := fetchEnvironmentAndVerify(t, clients, WithEnv(corev1.EnvVar{
			Name: test.EnvKey,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: test.ConformanceConfigMap,
					},
					Key: test.EnvKey,
				},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("envFrom", func(t *testing.T) {
		err := fetchEnvironmentAndVerify(t, clients, WithEnvFrom(corev1.EnvFromSource{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: test.ConformanceConfigMap,
				},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
	})
}

func fetchEnvironmentAndVerify(t *testing.T, clients *test.Clients, opts ...interface{}) error {
	_, ri, err := fetchRuntimeInfo(t, clients, opts...)
	if err != nil {
		return err
	}

	if value, ok := ri.Host.EnvVars[test.EnvKey]; ok {
		if value != test.EnvValue {
			return fmt.Errorf("environment value doesn't match. Expected: %s, Found: %s", test.EnvValue, value)
		}
	} else {
		return fmt.Errorf("%s not found in environment variables", test.EnvKey)
	}
	return nil
}
