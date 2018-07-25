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

	"go.uber.org/zap"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *zap.SugaredLogger, clients *Clients, names ResourceNames) error {
	route := Route(Flags.Namespace, names)
	// Log the route object
	if routeJSON, err := json.Marshal(route); err != nil {
		logger.Infof("Failed to create json from route object")
	} else {
		logger.Infow("Created resource object", "ROUTE", string(routeJSON))
	}

	_, err := clients.Routes.Create(route)
	return err
}
