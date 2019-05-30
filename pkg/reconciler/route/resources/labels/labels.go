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

package labels

import (
	"github.com/knative/serving/pkg/reconciler/route/config"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func IsObjectLocalVisibility(meta v1.ObjectMeta) bool {
	return meta.Labels != nil && meta.Labels[config.VisibilityLabelKey] != ""
}

func SetVisibility(meta *v1.ObjectMeta, isClusterLocal bool) {
	if isClusterLocal {
		SetLabel(meta, config.VisibilityLabelKey, config.VisibilityClusterLocal)
	} else {
		DeleteLabel(meta, config.VisibilityLabelKey)
	}
}

func SetLabel(meta *v1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}

	meta.Labels[key] = value
}

func DeleteLabel(meta *v1.ObjectMeta, key string) {
	if meta.Labels != nil {
		delete(meta.Labels, key)
	}
}
