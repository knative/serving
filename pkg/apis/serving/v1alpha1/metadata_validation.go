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

package v1alpha1

import (
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/webhook"
	"strings"
)

func validateMetadata(new webhook.GenericCRD) *apis.FieldError {
	name := new.GetObjectMeta().GetName()

	if strings.Contains(name, ".") {
		return &apis.FieldError{
			Message: "Invalid resource name: special character . must not be present",
			Paths:   []string{"name"},
		}
	}

	if len(name) > 63 {
		return &apis.FieldError{
			Message: "Invalid resource name: length must be no more than 63 characters",
			Paths:   []string{"name"},
		}
	}
	return nil
}
