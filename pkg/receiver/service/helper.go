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
	"reflect"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging/logkey"
)

// loggerWithServiceInfo enriches the logs with service name and namespace.
func loggerWithServiceInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Service, name))
}

func (c *Receiver) updateStatus(service *v1alpha1.Service) (*v1alpha1.Service, error) {
	serviceClient := c.ElaClientSet.ServingV1alpha1().Services(service.Namespace)
	existing, err := serviceClient.Get(service.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Status, service.Status) {
		existing.Status = service.Status
		// TODO: for CRD there's no updatestatus, so use normal update.
		return serviceClient.Update(existing)
	}
	return existing, nil
}
