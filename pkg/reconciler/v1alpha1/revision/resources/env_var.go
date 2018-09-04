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

package resources

import (
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	knativeRevisionEnvVariableKey      = "KNATIVE_REVISION_NAME"
	knativeConfigurationEnvVariableKey = "KNATIVE_CONFIGURATION_NAME"
	knativeServiceEnvVariableKey       = "KNATIVE_SERVICE_NAME"
)

// GetKnativeEnvVar create an EnvVar object containing revison, service and configuraiton names
func GetKnativeEnvVar(rev *v1alpha1.Revision) []corev1.EnvVar {

	revLabels := rev.Labels

	revNameEnvVar := corev1.EnvVar{
		Name:  knativeRevisionEnvVariableKey,
		Value: rev.Name,
	}

	configNameEnvVar := corev1.EnvVar{
		Name:  knativeConfigurationEnvVariableKey,
		Value: revLabels[serving.ConfigurationLabelKey],
	}

	serviceNameEnvVar := corev1.EnvVar{
		Name:  knativeServiceEnvVariableKey,
		Value: revLabels[serving.ServiceLabelKey],
	}

	envVars := []corev1.EnvVar{revNameEnvVar, configNameEnvVar, serviceNameEnvVar}

	return envVars
}
