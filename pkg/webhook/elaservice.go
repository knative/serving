/*
Copyright 2018 Google LLC. All Rights Reserved.
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
	"errors"

	"github.com/golang/glog"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
)

const (
	errInvalidRevisionsMessage        = "Exactly one of revision or revisiontemplate must be specified in traffic field"
	errInvalidTargetPercentSumMessage = "Traffic percent sum is not equal to 100"
	errNegativeTargetPercentMessage   = "Traffic percent can not be negative"
)

// ValidateElaService is ElaService resource specific validation and mutation handler
func ValidateElaService(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
	var oldES *v1alpha1.ElaService
	if old != nil {
		var ok bool
		oldES, ok = old.(*v1alpha1.ElaService)
		if !ok {
			return errors.New("Failed to convert old into ElaService")
		}
	}
	glog.Infof("ValidateElaService: OLD ElaService is\n%+v", oldES)
	newES, ok := new.(*v1alpha1.ElaService)
	if !ok {
		return errors.New("Failed to convert new into ElaService")
	}
	glog.Infof("ValidateElaService: NEW ElaService is\n%+v", newES)

	if err := validateTrafficTarget(newES); err != nil {
		return err
	}

	return nil
}

func validateTrafficTarget(elaService *v1alpha1.ElaService) error {
	// A service as a placeholder that's not backed by anything is allowed.
	if elaService.Spec.Traffic == nil {
		return nil
	}

	percentSum := 0
	for _, trafficTarget := range elaService.Spec.Traffic {
		revisionLen := len(trafficTarget.Revision)
		revisionTemplateLen := len(trafficTarget.RevisionTemplate)
		if (revisionLen == 0 && revisionTemplateLen == 0) ||
			(revisionLen != 0 && revisionTemplateLen != 0) {
			return errors.New(errInvalidRevisionsMessage)
		}

		if trafficTarget.Percent < 0 {
			return errors.New(errNegativeTargetPercentMessage)
		}
		percentSum += trafficTarget.Percent
	}

	if percentSum != 100 {
		return errors.New(errInvalidTargetPercentSumMessage)
	}
	return nil
}
