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

package v1beta1

import (
	"context"
	"fmt"

	"github.com/knative/pkg/apis"
)

// ConvertUp implements apis.Convertible
func (source *Revision) ConvertUp(ctx context.Context, sink apis.Convertible) error {
	return fmt.Errorf("v1beta1 is the highest known version, got: %T", sink)
}

// ConvertDown implements apis.Convertible
func (sink *Revision) ConvertDown(ctx context.Context, source apis.Convertible) error {
	return fmt.Errorf("v1beta1 is the highest known version, got: %T", source)
}
