/*
Copyright 2019 The Knative Authors.

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
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

// ConvertTo implements apis.Convertible
func (source *Route) ConvertTo(ctx context.Context, obj apis.Convertible) error {
	switch sink := obj.(type) {
	case *v1beta1.Route:
		sink.ObjectMeta = source.ObjectMeta
		source.Status.ConvertTo(apis.WithinStatus(ctx), &sink.Status)
		return source.Spec.ConvertTo(apis.WithinSpec(ctx), &sink.Spec)
	default:
		return apis.ConvertToViaProxy(ctx, source, &v1beta1.Route{}, sink)
	}
}

// ConvertTo helps implement apis.Convertible
func (source *RouteSpec) ConvertTo(ctx context.Context, sink *v1.RouteSpec) error {
	sink.Traffic = make([]v1.TrafficTarget, len(source.Traffic))
	for i := range source.Traffic {
		if err := source.Traffic[i].ConvertTo(ctx, &sink.Traffic[i]); err != nil {
			return err
		}
	}
	return nil
}

// ConvertTo helps implement apis.Convertible
func (source *TrafficTarget) ConvertTo(ctx context.Context, sink *v1.TrafficTarget) error {
	*sink = source.TrafficTarget
	switch {
	case source.Tag != "" && source.DeprecatedName != "":
		if apis.IsInSpec(ctx) {
			return apis.ErrMultipleOneOf("name", "tag")
		}
	case source.DeprecatedName != "":
		sink.Tag = source.DeprecatedName
	}
	return nil
}

// ConvertTo helps implement apis.Convertible
func (source *RouteStatus) ConvertTo(ctx context.Context, sink *v1.RouteStatus) {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsRouteCondition)
	source.RouteStatusFields.ConvertTo(ctx, &sink.RouteStatusFields)
}

// ConvertTo helps implement apis.Convertible
func (source *RouteStatusFields) ConvertTo(ctx context.Context, sink *v1.RouteStatusFields) {
	if source.URL != nil {
		sink.URL = source.URL.DeepCopy()
	}

	if source.Address != nil {
		if sink.Address == nil {
			sink.Address = &duckv1.Addressable{}
		}
		source.Address.ConvertTo(ctx, sink.Address)
	}

	sink.Traffic = make([]v1.TrafficTarget, len(source.Traffic))
	for i := range source.Traffic {
		source.Traffic[i].ConvertTo(ctx, &sink.Traffic[i])
	}
}

// ConvertFrom implements apis.Convertible
func (sink *Route) ConvertFrom(ctx context.Context, obj apis.Convertible) error {
	switch source := obj.(type) {
	case *v1beta1.Route:
		sink.ObjectMeta = source.ObjectMeta
		sink.Spec.ConvertFrom(ctx, source.Spec)
		sink.Status.ConvertFrom(ctx, source.Status)
		return nil
	default:
		return apis.ConvertFromViaProxy(ctx, source, &v1beta1.Route{}, sink)
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *RouteSpec) ConvertFrom(ctx context.Context, source v1.RouteSpec) {
	sink.Traffic = make([]TrafficTarget, len(source.Traffic))
	for i := range source.Traffic {
		sink.Traffic[i].ConvertFrom(ctx, source.Traffic[i])
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *TrafficTarget) ConvertFrom(ctx context.Context, source v1.TrafficTarget) {
	sink.TrafficTarget = source
}

// ConvertFrom helps implement apis.Convertible
func (sink *RouteStatus) ConvertFrom(ctx context.Context, source v1.RouteStatus) {
	source.ConvertTo(ctx, &sink.Status, v1.IsRouteCondition)
	sink.RouteStatusFields.ConvertFrom(ctx, source.RouteStatusFields)
}

// ConvertFrom helps implement apis.Convertible
func (sink *RouteStatusFields) ConvertFrom(ctx context.Context, source v1.RouteStatusFields) {
	if source.URL != nil {
		sink.URL = source.URL.DeepCopy()
	}

	if source.Address != nil {
		if sink.Address == nil {
			sink.Address = &duckv1alpha1.Addressable{}
		}
		sink.Address.ConvertFrom(ctx, source.Address)
	}

	sink.Traffic = make([]TrafficTarget, len(source.Traffic))
	for i := range source.Traffic {
		sink.Traffic[i].ConvertFrom(ctx, source.Traffic[i])
	}
}
