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

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

// ConvertTo implements apis.Convertible
func (source *Revision) ConvertTo(ctx context.Context, obj apis.Convertible) error {
	switch sink := obj.(type) {
	case *v1beta1.Revision:
		sink.ObjectMeta = source.ObjectMeta
		source.Status.ConvertTo(ctx, &sink.Status)
		return source.Spec.ConvertTo(ctx, &sink.Spec)
	default:
		return apis.ConvertToViaProxy(ctx, source, &v1beta1.Revision{}, sink)
	}
}

// ConvertTo helps implement apis.Convertible
func (source *RevisionTemplateSpec) ConvertTo(ctx context.Context, sink *v1.RevisionTemplateSpec) error {
	sink.ObjectMeta = source.ObjectMeta
	return source.Spec.ConvertTo(ctx, &sink.Spec)
}

// ConvertTo helps implement apis.Convertible
func (source *RevisionSpec) ConvertTo(ctx context.Context, sink *v1.RevisionSpec) error {
	if source.TimeoutSeconds != nil {
		sink.TimeoutSeconds = ptr.Int64(*source.TimeoutSeconds)
	}
	if source.ContainerConcurrency != nil {
		sink.ContainerConcurrency = ptr.Int64(*source.ContainerConcurrency)
	}
	switch {
	case source.DeprecatedContainer != nil && len(source.Containers) > 0:
		return apis.ErrMultipleOneOf("container", "containers")
	case source.DeprecatedContainer != nil:
		sink.PodSpec = corev1.PodSpec{
			ServiceAccountName: source.ServiceAccountName,
			Containers:         []corev1.Container{*source.DeprecatedContainer},
			Volumes:            source.Volumes,
			ImagePullSecrets:   source.ImagePullSecrets,
		}
	case len(source.Containers) != 0:
		sink.PodSpec = source.PodSpec
	default:
		return apis.ErrMissingOneOf("container", "containers")
	}
	return nil
}

// ConvertTo helps implement apis.Convertible
func (source *RevisionStatus) ConvertTo(ctx context.Context, sink *v1.RevisionStatus) {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsRevisionCondition)
	sink.ServiceName = source.ServiceName
	sink.LogURL = source.LogURL
	sink.DeprecatedImageDigest = source.DeprecatedImageDigest
	sink.ContainerStatuses = make([]v1.ContainerStatus, len(source.ContainerStatuses))
	for i := range source.ContainerStatuses {
		source.ContainerStatuses[i].ConvertTo(ctx, &sink.ContainerStatuses[i])
	}
}

// ConvertTo helps implement apis.Convertible
func (source *ContainerStatus) ConvertTo(ctx context.Context, sink *v1.ContainerStatus) {
	sink.Name = source.Name
	sink.ImageDigest = source.ImageDigest
}

// ConvertFrom implements apis.Convertible
func (sink *Revision) ConvertFrom(ctx context.Context, obj apis.Convertible) error {
	switch source := obj.(type) {
	case *v1beta1.Revision:
		sink.ObjectMeta = source.ObjectMeta
		sink.Status.ConvertFrom(ctx, source.Status)
		return sink.Spec.ConvertFrom(ctx, source.Spec)
	default:
		return apis.ConvertFromViaProxy(ctx, source, &v1beta1.Revision{}, sink)
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *RevisionTemplateSpec) ConvertFrom(ctx context.Context, source v1.RevisionTemplateSpec) error {
	sink.ObjectMeta = source.ObjectMeta
	return sink.Spec.ConvertFrom(ctx, source.Spec)
}

// ConvertFrom helps implement apis.Convertible
func (sink *RevisionSpec) ConvertFrom(ctx context.Context, source v1.RevisionSpec) error {
	sink.RevisionSpec = *source.DeepCopy()
	return nil
}

// ConvertFrom helps implement apis.Convertible
func (sink *RevisionStatus) ConvertFrom(ctx context.Context, source v1.RevisionStatus) {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsRevisionCondition)
	sink.ServiceName = source.ServiceName
	sink.LogURL = source.LogURL
	sink.DeprecatedImageDigest = source.DeprecatedImageDigest
	sink.ContainerStatuses = make([]ContainerStatus, len(source.ContainerStatuses))
	for i := range sink.ContainerStatuses {
		sink.ContainerStatuses[i].ConvertFrom(ctx, &source.ContainerStatuses[i])
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *ContainerStatus) ConvertFrom(ctx context.Context, source *v1.ContainerStatus) {
	sink.Name = source.Name
	sink.ImageDigest = source.ImageDigest
}
