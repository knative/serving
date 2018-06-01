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
	"context"
	"errors"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/mattbaird/jsonpatch"
)

var (
	errInvalidRevisions        = errors.New("the route must have exactly one of revisionName or configurationName in traffic field")
	errInvalidRouteInput       = errors.New("failed to convert input into Route")
	errInvalidTargetPercentSum = errors.New("the route must have traffic percent sum equal to 100")
	errNegativeTargetPercent   = errors.New("the route cannot have a negative traffic percent")
	errTrafficTargetsNotUnique = errors.New("the traffic targets must be unique")
)

// ValidateRoute is Route resource specific validation and mutation handler
func ValidateRoute(ctx context.Context) ResourceCallback {
	return func(patches *[]jsonpatch.JsonPatchOperation, old GenericCRD, new GenericCRD) error {
		_, newRoute, err := unmarshalRoutes(ctx, old, new, "ValidateRoute")
		if err != nil {
			return err
		}
		if err := validateTrafficTarget(newRoute); err != nil {
			return err
		}
		if err := validateUniqueTrafficTarget(newRoute); err != nil {
			return err
		}

		return nil
	}
}

func validateTrafficTarget(route *v1alpha1.Route) error {
	// A service as a placeholder that's not backed by anything is allowed.
	if route.Spec.Traffic == nil {
		return nil
	}

	percentSum := 0
	for _, trafficTarget := range route.Spec.Traffic {
		revisionLen := len(trafficTarget.RevisionName)
		configurationLen := len(trafficTarget.ConfigurationName)
		if (revisionLen == 0 && configurationLen == 0) ||
			(revisionLen != 0 && configurationLen != 0) {
			return errInvalidRevisions
		}

		if trafficTarget.Percent < 0 {
			return errNegativeTargetPercent
		}
		percentSum += trafficTarget.Percent
	}

	if percentSum != 100 {
		return errInvalidTargetPercentSum
	}
	return nil
}

func validateUniqueTrafficTarget(route *v1alpha1.Route) error {
	if route.Spec.Traffic == nil {
		return nil
	}

	type tt struct {
		revision      string
		configuration string
	}
	trafficMap := make(map[string]tt)
	for _, trafficTarget := range route.Spec.Traffic {
		if trafficTarget.Name == "" {
			continue
		}

		traffic := tt{
			revision:      trafficTarget.RevisionName,
			configuration: trafficTarget.ConfigurationName,
		}

		if _, ok := trafficMap[trafficTarget.Name]; !ok {
			trafficMap[trafficTarget.Name] = traffic
		} else if trafficMap[trafficTarget.Name] != traffic {
			return errTrafficTargetsNotUnique
		}
	}
	return nil
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
