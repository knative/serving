/*
Copyright 2019 The Knative Authors

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
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/apis"
)

// Validate inspects and validates ClusterServerlessService object.
func (ss *ServerlessService) Validate(ctx context.Context) (errs *apis.FieldError) {
	return errs.Also(networking.ValidateAnnotations(ss.ObjectMeta.GetAnnotations()).ViaField("annotations")).
		Also(ss.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
}

// Validate inspects and validates ServerlessServiceSpec object.
func (sss *ServerlessServiceSpec) Validate(ctx context.Context) *apis.FieldError {
	// Spec must not be empty.
	if equality.Semantic.DeepEqual(sss, &ServerlessServiceSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError
	// Spec mode must be from the enum
	switch sss.Mode {
	case SKSOperationModeProxy, SKSOperationModeServe:
		break
	case "":
		all = all.Also(apis.ErrMissingField("mode"))
	default:
		all = all.Also(apis.ErrInvalidValue(sss.Mode, "mode"))
	}

	if sss.NumActivators < 0 {
		all = all.Also(apis.ErrInvalidValue(sss.NumActivators, "numActivators"))
	}

	all = all.Also(networking.ValidateNamespacedObjectReference(&sss.ObjectRef).ViaField("objectRef"))

	return all.Also(sss.ProtocolType.Validate(ctx).ViaField("protocolType"))
}
