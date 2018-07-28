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

// configuration.go provides methods to perform actions on the configuration object.

package test

import (
	"encoding/json"

	"go.uber.org/zap"
)

// CreateConfiguration create a configuration resource in namespace with the name names.Config
// that uses the image specified by imagePath.
func CreateConfiguration(logger *zap.SugaredLogger, clients *Clients, names ResourceNames, imagePath string) error {
	config := Configuration(Flags.Namespace, names, imagePath)
	// Log config object
	if configJSON, err := json.Marshal(config); err != nil {
		logger.Infof("Failed to create json from route object")
	} else {
		logger.Infow("Created resource object", "CONFIGURATION", string(configJSON))
	}
	_, err := clients.Configs.Create(config)
	return err
}
