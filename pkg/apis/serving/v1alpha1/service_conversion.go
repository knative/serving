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

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

// ConvertTo implements apis.Convertible
func (source *Service) ConvertTo(ctx context.Context, obj apis.Convertible) error {
	switch sink := obj.(type) {
	case *v1beta1.Service:
		sink.ObjectMeta = source.ObjectMeta
		if err := source.Spec.ConvertTo(ctx, &sink.Spec); err != nil {
			return err
		}
		return source.Status.ConvertTo(ctx, &sink.Status)
	default:
		return apis.ConvertToViaProxy(ctx, source, &v1beta1.Service{}, sink)
	}
}

// ConvertTo helps implement apis.Convertible
func (source *ServiceSpec) ConvertTo(ctx context.Context, sink *v1.ServiceSpec) error {
	switch {
	case source.DeprecatedRunLatest != nil:
		sink.RouteSpec = v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				Percent:        ptr.Int64(100),
				LatestRevision: ptr.Bool(true),
			}},
		}
		return source.DeprecatedRunLatest.Configuration.ConvertTo(ctx, &sink.ConfigurationSpec)

	case source.DeprecatedRelease != nil:
		if len(source.DeprecatedRelease.Revisions) == 2 {
			sink.RouteSpec = v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					RevisionName: source.DeprecatedRelease.Revisions[0],
					Percent:      ptr.Int64(int64(100 - source.DeprecatedRelease.RolloutPercent)),
					Tag:          "current",
				}, {
					RevisionName: source.DeprecatedRelease.Revisions[1],
					Percent:      ptr.Int64(int64(source.DeprecatedRelease.RolloutPercent)),
					Tag:          "candidate",
				}, {
					Percent:        nil,
					Tag:            "latest",
					LatestRevision: ptr.Bool(true),
				}},
			}
		} else {
			sink.RouteSpec = v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					RevisionName: source.DeprecatedRelease.Revisions[0],
					Percent:      ptr.Int64(100),
					Tag:          "current",
				}, {
					Percent:        nil,
					Tag:            "latest",
					LatestRevision: ptr.Bool(true),
				}},
			}
		}
		for i, tt := range sink.RouteSpec.Traffic {
			if tt.RevisionName == "@latest" {
				sink.RouteSpec.Traffic[i].RevisionName = ""
				sink.RouteSpec.Traffic[i].LatestRevision = ptr.Bool(true)
			}
		}
		return source.DeprecatedRelease.Configuration.ConvertTo(ctx, &sink.ConfigurationSpec)

	case source.DeprecatedPinned != nil:
		sink.RouteSpec = v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				RevisionName: source.DeprecatedPinned.RevisionName,
				Percent:      ptr.Int64(100),
			}},
		}
		return source.DeprecatedPinned.Configuration.ConvertTo(ctx, &sink.ConfigurationSpec)

	case source.DeprecatedManual != nil:
		return ConvertErrorf("manual", "manual mode cannot be migrated forward.")

	default:
		source.RouteSpec.ConvertTo(ctx, &sink.RouteSpec)
		return source.ConfigurationSpec.ConvertTo(ctx, &sink.ConfigurationSpec)
	}
}

// ConvertTo helps implement apis.Convertible
func (source *ServiceStatus) ConvertTo(ctx context.Context, sink *v1.ServiceStatus) error {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsServiceCondition)
	source.RouteStatusFields.ConvertTo(ctx, &sink.RouteStatusFields)
	return source.ConfigurationStatusFields.ConvertTo(ctx, &sink.ConfigurationStatusFields)
}

// ConvertFrom implements apis.Convertible
func (sink *Service) ConvertFrom(ctx context.Context, obj apis.Convertible) error {
	switch source := obj.(type) {
	case *v1beta1.Service:
		sink.ObjectMeta = source.ObjectMeta
		if err := sink.Spec.ConvertFrom(ctx, source.Spec); err != nil {
			return err
		}
		return sink.Status.ConvertFrom(ctx, source.Status)
	default:
		return apis.ConvertFromViaProxy(ctx, source, &v1beta1.Service{}, sink)
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *ServiceSpec) ConvertFrom(ctx context.Context, source v1.ServiceSpec) error {
	sink.RouteSpec.ConvertFrom(ctx, source.RouteSpec)
	return sink.ConfigurationSpec.ConvertFrom(ctx, source.ConfigurationSpec)
}

// ConvertFrom helps implement apis.Convertible
func (sink *ServiceStatus) ConvertFrom(ctx context.Context, source v1.ServiceStatus) error {
	source.ConvertTo(ctx, &sink.Status, v1.IsServiceCondition)
	sink.RouteStatusFields.ConvertFrom(ctx, source.RouteStatusFields)
	return sink.ConfigurationStatusFields.ConvertFrom(ctx, source.ConfigurationStatusFields)
}
