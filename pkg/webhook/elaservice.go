/*
Copyright 2017 Google Inc. All Rights Reserved.
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
	"fmt"

	"github.com/golang/glog"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

// ValidateElaService is ElaService resource specific validation and mutation handler
func ValidateElaService(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	var oldES *v1alpha1.ElaService

	if old != nil {
		var ok bool
		oldES, ok = old.(*v1alpha1.ElaService)
		if !ok {
			return fmt.Errorf("Failed to convert old into ElaService")
		}
	}
	glog.Infof("ValidateElaService: OLD ElaService is\n%+v", oldES)
	newES, ok := new.(*v1alpha1.ElaService)
	if !ok {
		return fmt.Errorf("Failed to convert new into ElaService")
	}
	glog.Infof("ValidateElaService: NEW ElaService is\n%+v", newES)
	return nil
}
