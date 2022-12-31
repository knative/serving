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
	"knative.dev/serving/pkg/apis/serving"
)

// SetDefaults implements apis.Defaultable
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

// SetDefaults implements apis.Defaultable
func (cs *ConfigurationSpec) SetDefaults(ctx context.Context) {
	if apis.IsInUpdate(ctx) {
		var prev ConfigurationSpec
		prevConfig, ok := apis.GetBaseline(ctx).(*Configuration)
		if ok {
			prev = prevConfig.Spec
		} else {
			prevSvc, ok := apis.GetBaseline(ctx).(*Service)
			if !ok {
				panic("expected a Configuration or Service baseline")
			}
			prev = prevSvc.Spec.ConfigurationSpec
		}
		newName := cs.Template.ObjectMeta.Name
		oldName := prev.Template.ObjectMeta.Name
		if newName != "" && newName == oldName {
			// Skip defaulting, to avoid suggesting changes that would conflict with
			// "BYO RevisionName".
			return
		}
	}
	cs.Template.SetDefaults(ctx)
}
