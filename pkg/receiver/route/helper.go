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

package route

import (
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
)

// loggerWithRouteInfo enriches the logs with route name and namespace.
func loggerWithRouteInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Route, name))
}

func (c *Receiver) routeDomain(route *v1alpha1.Route) string {
	domain := c.controllerConfig.LookupDomainForLabels(route.ObjectMeta.Labels)
	return fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, domain)
}

func (c *Receiver) updateStatus(route *v1alpha1.Route) (*v1alpha1.Route, error) {
	routeClient := c.ElaClientSet.ServingV1alpha1().Routes(route.Namespace)
	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Status, route.Status) {
		existing.Status = route.Status
		// TODO: for CRD there's no updatestatus, so use normal update.
		return routeClient.Update(existing)
	}
	return existing, nil
}
