/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// route.go provides methods to perform actions on the route resource.

package test

import (
	"encoding/json"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"go.uber.org/zap"
)

func logRoute(logger *zap.SugaredLogger, route *v1alpha1.Route) {
	// Log the route object
	if routeJSON, err := json.Marshal(route); err != nil {
		logger.Infof("Failed to create json from route object: %v", err)
	} else {
		LogResourceObject(logger, "ROUTE", string(routeJSON))
	}
}

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *zap.SugaredLogger, clients *Clients, names ResourceNames) error {
	route := Route(Flags.Namespace, names)
	logRoute(logger, route)
	_, err := clients.Routes.Create(route)
	return err
}

// CreateBlueGreenRoute creates a route in the given namespace using the route name in names.
// Traffic is evenly split between the two routes specified by blue and green.
func CreateBlueGreenRoute(logger *zap.SugaredLogger, clients *Clients, names, blue, green ResourceNames) error {
	route := BlueGreenRoute(Flags.Namespace, names, blue, green)
	logRoute(logger, route)
	_, err := clients.Routes.Create(route)
	return err
}
