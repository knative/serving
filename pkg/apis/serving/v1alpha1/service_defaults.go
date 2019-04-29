/*
Copyright 2018 The Knative Authors

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

	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

func (s *Service) SetDefaults(ctx context.Context) {
	s.Spec.SetDefaults(apis.WithinSpec(ctx))

	if ui := apis.GetUserInfo(ctx); ui != nil {
		ans := s.GetAnnotations()
		if ans == nil {
			ans = map[string]string{}
			defer s.SetAnnotations(ans)
		}

		if apis.IsInUpdate(ctx) {
			old := apis.GetBaseline(ctx).(*Service)
			if equality.Semantic.DeepEqual(old.Spec, s.Spec) {
				return
			}
			ans[serving.UpdaterAnnotation] = ui.Username
		} else {
			ans[serving.CreatorAnnotation] = ui.Username
			ans[serving.UpdaterAnnotation] = ui.Username
		}
	}
}

func (ss *ServiceSpec) SetDefaults(ctx context.Context) {
	if v1beta1.IsUpgradeViaDefaulting(ctx) {
		beta := v1beta1.ServiceSpec{}
		if ss.ConvertUp(ctx, &beta) == nil {
			alpha := ServiceSpec{}
			if alpha.ConvertDown(ctx, beta) == nil {
				*ss = alpha
			}
		}
	}

	if ss.DeprecatedRunLatest != nil {
		ss.DeprecatedRunLatest.Configuration.SetDefaults(ctx)
	} else if ss.DeprecatedPinned != nil {
		ss.DeprecatedPinned.Configuration.SetDefaults(ctx)
	} else if ss.DeprecatedRelease != nil {
		ss.DeprecatedRelease.Configuration.SetDefaults(ctx)
	} else if ss.DeprecatedManual != nil {
	} else {
		ss.ConfigurationSpec.SetDefaults(ctx)
		ss.RouteSpec.SetDefaults(v1beta1.WithDefaultConfigurationName(ctx))
	}
}
