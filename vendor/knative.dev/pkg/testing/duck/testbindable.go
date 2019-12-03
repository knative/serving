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

package duck

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	"knative.dev/pkg/tracker"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TestBindable is a simple resource that's compatible with our webhook
type TestBindable struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TestBindableSpec   `json:"spec,omitempty"`
	Status TestBindableStatus `json:"status,omitempty"`
}

// Check that TestBindable may be validated and defaulted.
var _ apis.Listable = (*TestBindable)(nil)
var _ duck.Bindable = (*TestBindable)(nil)

// TestBindableSpec represents test resource spec.
type TestBindableSpec struct {
	duckv1alpha1.BindingSpec `json:",inline"`

	Foo string `json:"foo"`
}

// TestBindableStatus represents the status of our test binding.
type TestBindableStatus struct {
	duckv1.Status `json:",inline"`
}

// GetGroupVersionKind returns the GroupVersionKind.
func (tb *TestBindable) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("TestBindable")
}

// GetUntypedSpec returns the spec of the resource.
func (tb *TestBindable) GetUntypedSpec() interface{} {
	return tb.Spec
}

// GetListType implements apis.Listable
func (tb *TestBindable) GetListType() runtime.Object {
	return &TestBindableList{}
}

// GetSubject implements psbinding.Bindable
func (tb *TestBindable) GetSubject() tracker.Reference {
	return tb.Spec.Subject
}

// GetBindingStatus implements psbinding.Bindable
func (tb *TestBindable) GetBindingStatus() duck.BindableStatus {
	return &tb.Status
}

// SetObservedGeneration implements psbinding.BindableStatus
func (tbs *TestBindableStatus) SetObservedGeneration(gen int64) {
	tbs.ObservedGeneration = gen
}

// Do implements psbinding.Bindable
func (tb *TestBindable) Do(ctx context.Context, ps *duckv1.WithPod) {
	// First undo so that we can just unconditionally append below.
	tb.Undo(ctx, ps)

	spec := ps.Spec.Template.Spec
	for i := range spec.InitContainers {
		spec.InitContainers[i].Env = append(spec.InitContainers[i].Env, corev1.EnvVar{
			Name:  "FOO",
			Value: tb.Spec.Foo,
		})
	}
	for i := range spec.Containers {
		spec.Containers[i].Env = append(spec.Containers[i].Env, corev1.EnvVar{
			Name:  "FOO",
			Value: tb.Spec.Foo,
		})
	}
}

func (tb *TestBindable) Undo(ctx context.Context, ps *duckv1.WithPod) {
	spec := ps.Spec.Template.Spec
	for i, c := range spec.InitContainers {
		for j, ev := range c.Env {
			if ev.Name == "FOO" {
				spec.InitContainers[i].Env = append(spec.InitContainers[i].Env[:j], spec.InitContainers[i].Env[j+1:]...)
				break
			}
		}
	}
	for i, c := range spec.Containers {
		for j, ev := range c.Env {
			if ev.Name == "FOO" {
				spec.Containers[i].Env = append(spec.Containers[i].Env[:j], spec.Containers[i].Env[j+1:]...)
				break
			}
		}
	}
}

var tbCondSet = apis.NewLivingConditionSet()

// clearLTT clears the last transition time from our conditions because
// they are hard to ignore in the context of *unstructured.Unstructured,
// which is how psbinding updates statuses.
func (tbs *TestBindableStatus) clearLTT() {
	for i := range tbs.Conditions {
		tbs.Conditions[i].LastTransitionTime = apis.VolatileTime{}
	}
}

// InitializeConditions populates the TestBindableStatus's conditions field
// with all of its conditions configured to Unknown.
func (tbs *TestBindableStatus) InitializeConditions() {
	tbCondSet.Manage(tbs).InitializeConditions()
	tbs.clearLTT()
}

// MarkBindingUnavailable marks the TestBindable's Ready condition to False with
// the provided reason and message.
func (tbs *TestBindableStatus) MarkBindingUnavailable(reason, message string) {
	tbCondSet.Manage(tbs).MarkFalse(apis.ConditionReady, reason, message)
	tbs.clearLTT()
}

// MarkBindingAvailable marks the TestBindable's Ready condition to True.
func (tbs *TestBindableStatus) MarkBindingAvailable() {
	tbCondSet.Manage(tbs).MarkTrue(apis.ConditionReady)
	tbs.clearLTT()
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TestBindableList is a list of TestBindable resources
type TestBindableList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TestBindable `json:"items"`
}
