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

// patch contains logic to manipulate json patches.

package conformance

import (
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"k8s.io/apimachinery/pkg/util/json"
)

// GetChangedConfigurationBytes will marshal the origConfig and the newConfig to
// json, then return a json patch containing the difference. The returned patch
// can be applied as a json patch update to a Configuration.
func GetChangedConfigurationBytes(origConfig *v1alpha1.Configuration, newConfig *v1alpha1.Configuration) ([]byte, error) {
	origData, err := json.Marshal(origConfig)
	if err != nil {
		return nil, err
	}
	newData, err := json.Marshal(newConfig)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.CreateMergePatch(origData, newData)
	return patch, err
}
