/*
Copyright 2024 The Knative Authors

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

	"knative.dev/pkg/apis"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// ConvertTo implements apis.Convertible
// Converts source (v1beta1.DomainMapping) to sink (v1.DomainMapping).
func (source *DomainMapping) ConvertTo(ctx context.Context, sink apis.Convertible) error {
	switch sink := sink.(type) {
	case *v1.DomainMapping:
		sink.ObjectMeta = source.ObjectMeta
		sink.Status.Status = source.Status.Status
		sink.Status.URL = source.Status.URL
		sink.Status.Address = source.Status.Address
		sink.Spec.Ref = source.Spec.Ref
		if source.Spec.TLS != nil {
			sink.Spec.TLS = &v1.SecretTLS{
				SecretName: source.Spec.TLS.SecretName,
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown version, got: %T", sink)
	}
}

// ConvertFrom implements apis.Convertible
// Converts sink (v1.DomainMapping) to source (v1beta1.DomainMapping).
func (sink *DomainMapping) ConvertFrom(ctx context.Context, source apis.Convertible) error {
	switch source := source.(type) {
	case *v1.DomainMapping:
		sink.ObjectMeta = source.ObjectMeta
		sink.Status.Status = source.Status.Status
		sink.Status.URL = source.Status.URL
		sink.Status.Address = source.Status.Address
		sink.Spec.Ref = source.Spec.Ref
		if source.Spec.TLS != nil {
			sink.Spec.TLS = &SecretTLS{
				SecretName: source.Spec.TLS.SecretName,
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown version, got: %T", source)
	}
}
