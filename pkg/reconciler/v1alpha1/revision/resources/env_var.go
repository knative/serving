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
	"context"
	"encoding/json"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	corev1 "k8s.io/api/core/v1"
)

const (
	knativeEnvVariableKey = "KNATIVE_CONTEXT"
)

type environmentVar struct {
	RevisionName      string `json:"revision"`
	ServiceName       string `json:"service"`
	ConfigurationName string `json:"configuration"`
}

// GetKnativeEnvVar create an EnvVar object containing revison, service and configuraiton names
func GetKnativeEnvVar(ctx context.Context, rev *v1alpha1.Revision) corev1.EnvVar {

	logger := logging.FromContext(ctx)
	var configurationName string
	for _, revOwner := range rev.GetOwnerReferences() {
		if revOwner.Kind == "Configuration" {
			configurationName = revOwner.Name
			break
		}
	}

	knativeEnv := environmentVar{RevisionName: rev.Name,
		ServiceName:       names.K8sService(rev),
		ConfigurationName: configurationName,
	}

	jsonKnativeEnv, err := json.Marshal(knativeEnv)
	if err != nil {
		logger.Errorf("Error when marshalling envrionment variable for revision %q", rev.Name, err)
	}

	return corev1.EnvVar{
		Name:  knativeEnvVariableKey,
		Value: string(jsonKnativeEnv),
	}
}
