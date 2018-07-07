/*
Copyright 2018 The Knative Authors.
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
	"context"
	"errors"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRouteInput = errors.New("failed to convert input into Route")
)

// ValidateRoute is Route resource specific validation and mutation handler
func ValidateRoute(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		_, newRoute, err := unmarshalRoutes(ctx, old, new, "ValidateRoute")
		if err != nil {
			return err
		}

		// Can't just `return newRoute.Validate()` because it doesn't properly nil-check.
		if err := newRoute.Validate(); err != nil {
			return err
		}
		return nil
	}
}

func unmarshalRoutes(
	ctx context.Context, old GenericCRD, new GenericCRD, fnName string) (*v1alpha1.Route, *v1alpha1.Route, error) {
	logger := logging.FromContext(ctx)
	var oldRoute *v1alpha1.Route
	if old != nil {
		var ok bool
		oldRoute, ok = old.(*v1alpha1.Route)
		if !ok {
			return nil, nil, errInvalidRouteInput
		}
	}
	logger.Infof("%s: OLD Route is\n%+v", fnName, oldRoute)

	newRoute, ok := new.(*v1alpha1.Route)
	if !ok {
		return nil, nil, errInvalidRouteInput
	}
	logger.Infof("%s: NEW Route is\n%+v", fnName, newRoute)

	return oldRoute, newRoute, nil
}
