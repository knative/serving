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
	"go.uber.org/zap"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *zap.SugaredLogger, clients *Clients, names ResourceNames) error {
	route := Route(Flags.Namespace, names)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.Routes.Create(route)
	return err
}

// CreateBlueGreenRoute creates a route in the given namespace using the route name in names.
// Traffic is evenly split between the two routes specified by blue and green.
func CreateBlueGreenRoute(logger *zap.SugaredLogger, clients *Clients, names, blue, green ResourceNames) error {
	route := BlueGreenRoute(Flags.Namespace, names, blue, green)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.Routes.Create(route)
	return err
}
