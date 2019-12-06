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

	"knative.dev/pkg/apis"

	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func (c *Configuration) SetDefaults(ctx context.Context) {
	ctx = apis.WithinParent(ctx, c.ObjectMeta)
	c.Spec.SetDefaults(apis.WithinSpec(ctx))
	if c.GetOwnerReferences() == nil {
		if apis.IsInUpdate(ctx) {
			serving.SetUserInfo(ctx, apis.GetBaseline(ctx).(*Configuration).Spec, c.Spec, c)
		} else {
			serving.SetUserInfo(ctx, nil, c.Spec, c)
		}
	}
}

func (cs *ConfigurationSpec) SetDefaults(ctx context.Context) {
	if v1.IsUpgradeViaDefaulting(ctx) {
		v := v1.ConfigurationSpec{}
		if cs.ConvertUp(ctx, &v) == nil {
			alpha := ConfigurationSpec{}
			if alpha.ConvertDown(ctx, v) == nil {
				*cs = alpha
			}
		}
	}

	cs.GetTemplate().SetDefaults(ctx)
}
