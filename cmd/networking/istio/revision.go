/*
Copyright 2020 The Knative Authors

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

package main

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
)

// RevisionStub is stub for the versioned types for the purposes
// of defaulting
type RevisionStub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// TODO - add a general solution that captures
	// top level properties in addition to spec & status
	Spec   runtime.RawExtension `json:"spec"`
	Status runtime.RawExtension `json:"status"`
}

// SetDefaults will apply istio specific defaults to the RevisionStub
func (r *RevisionStub) SetDefaults(context.Context) {
	if r.Labels == nil {
		r.Labels = make(map[string]string)
	}

	var (
		parent string
		ok     bool

		parentKeys = []string{
			serving.ServiceLabelKey,
			serving.ConfigurationLabelKey,
		}
	)

	for _, parentKey := range parentKeys {
		parent, ok = r.Labels[parentKey]
		if ok {
			break
		}
	}

	// Shouldn't happen but you never know
	if !ok {
		return
	}

	r.Labels["service.istio.io/canonical-revision"] = r.Name
	r.Labels["service.istio.io/canonical-service"] = parent
}

// Validate sadly performs no validation
func (r *RevisionStub) Validate(context.Context) *apis.FieldError {
	return nil
}

// DeepCopyObject returns a deep copy of this object
func (r *RevisionStub) DeepCopyObject() runtime.Object {
	if r == nil {
		return nil
	}

	out := &RevisionStub{
		TypeMeta: r.TypeMeta, //simple struct
	}

	r.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	r.Spec.DeepCopyInto(&out.Spec)
	r.Status.DeepCopyInto(&out.Status)

	return out
}
