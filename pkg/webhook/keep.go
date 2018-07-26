/*
Copyright 2017 The Knative Authors

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

package webhook

import (
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// NewAdmissionController creates a new instance of the admission webhook controller.
func NewAdmissionController(client kubernetes.Interface, options ControllerOptions, logger *zap.SugaredLogger) (*AdmissionController, error) {
	return &AdmissionController{
		client:  client,
		options: options,
		// TODO(mattmoor): Will we need to rework these to support versioning?
		groupVersion: v1alpha1.SchemeGroupVersion,
		handlers: map[string]runtime.Object{
			"Revision":      &v1alpha1.Revision{},
			"Configuration": &v1alpha1.Configuration{},
			"Route":         &v1alpha1.Route{},
			"Service":       &v1alpha1.Service{},
		},
		logger: logger,
	}, nil
}
