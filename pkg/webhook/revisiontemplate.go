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
	"reflect"

	"github.com/golang/glog"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

const (
	emptySpecInRevisionTemplateErrorMessage = "The revision template must have revision template spec"
	emptyTemplateInSpecErrorMessage         = "The revision template spec must have revision template"
)

// ValidateRevisionTemplate is RevisionTemplate resource specific validation and mutation handler
func ValidateRevisionTemplate(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	var oldRT *v1alpha1.RevisionTemplate
	if old != nil {
		var ok bool
		oldRT, ok = old.(*v1alpha1.RevisionTemplate)
		if !ok {
			return fmt.Errorf("Failed to convert old into RevisionTemplate")
		}
	}
	glog.Infof("ValidateRevisionTemplate: OLD RevisionTemplate is\n%+v", oldRT)
	newRT, ok := new.(*v1alpha1.RevisionTemplate)
	if !ok {
		return fmt.Errorf("Failed to convert new into RevisionTemplate")
	}
	glog.Infof("ValidateRevisionTemplate: NEW RevisionTemplate is\n%+v", newRT)

	err := validateRevisionTemplate(newRT)
	if err != nil {
		return err
	}
	return nil
}

func validateRevisionTemplate(revisionTemplate *v1alpha1.RevisionTemplate) error {
	if reflect.DeepEqual(revisionTemplate.Spec, v1alpha1.RevisionTemplateSpec{}) {
		return fmt.Errorf(emptySpecInRevisionTemplateErrorMessage)
	}
	if reflect.DeepEqual(revisionTemplate.Spec.Template, v1alpha1.Revision{}) {
		return fmt.Errorf(emptyTemplateInSpecErrorMessage)
	}
	return nil
}
