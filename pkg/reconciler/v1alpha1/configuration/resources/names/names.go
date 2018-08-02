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

package names

import (
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func Revision(config *v1alpha1.Configuration) string {
	return fmt.Sprintf("%s-%05d", config.Name, config.Spec.Generation)
}

func Build(config *v1alpha1.Configuration) string {
	if config.Spec.Build == nil {
		return ""
	}
	return Revision(config)
}
