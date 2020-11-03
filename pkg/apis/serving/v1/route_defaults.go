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

package v1

import (
	"context"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
)

// SetDefaults implements apis.Defaultable
func (r *Route) SetDefaults(ctx context.Context) {
	r.Spec.SetDefaults(apis.WithinSpec(ctx))
	if r.GetOwnerReferences() == nil {
		if apis.IsInUpdate(ctx) {
			serving.SetUserInfo(ctx, apis.GetBaseline(ctx).(*Route).Spec, r.Spec, r)
		} else {
			serving.SetUserInfo(ctx, nil, r.Spec, r)
		}
	}
}

// SetDefaults implements apis.Defaultable
func (rs *RouteSpec) SetDefaults(ctx context.Context) {
	if len(rs.Traffic) == 0 && HasDefaultConfigurationName(ctx) {
		rs.Traffic = []TrafficTarget{{
			Percent:        ptr.Int64(100),
			LatestRevision: ptr.Bool(true),
		}}
	}

	for idx := range rs.Traffic {
		rs.Traffic[idx].SetDefaults(ctx)
	}
}

// SetDefaults implements apis.Defaultable
func (tt *TrafficTarget) SetDefaults(ctx context.Context) {
	if tt.LatestRevision == nil {
		tt.LatestRevision = ptr.Bool(tt.RevisionName == "")
	}
	// Despite the fact that we have the field percent
	// as required, historically we were lenient about checking this.
	// But by setting explicit `0` we can eliminate lots of checking
	// downstream in validation and controllers.
	if tt.Percent == nil {
		tt.Percent = ptr.Int64(0)
	}
}
