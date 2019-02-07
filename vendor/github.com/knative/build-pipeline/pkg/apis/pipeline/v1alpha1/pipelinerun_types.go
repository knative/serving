/*
Copyright 2018 The Knative Authors.

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
	"fmt"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/webhook"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	pipelineRunControllerName = "PipelineRun"
	groupVersionKind          = schema.GroupVersionKind{
		Group:   SchemeGroupVersion.Group,
		Version: SchemeGroupVersion.Version,
		Kind:    pipelineRunControllerName,
	}
)

// Assert that TaskRun implements the GenericCRD interface.
var _ webhook.GenericCRD = (*TaskRun)(nil)

// PipelineRunSpec defines the desired state of PipelineRun
type PipelineRunSpec struct {
	PipelineRef PipelineRef     `json:"pipelineRef"`
	Trigger     PipelineTrigger `json:"trigger"`
	// Resources is a list of bindings specifying which actual instances of
	// PipelineResources to use for the resources the Pipeline has declared
	// it needs.
	Resources []PipelineResourceBinding `json:"resources"`
	// +optional
	ServiceAccount string `json:"serviceAccount"`
	// +optional
	Results    *Results `json:"results,omitempty"`
	Generation int64    `json:"generation,omitempty"`
	// Used for cancelling a pipelinerun (and maybe more later on)
	// +optional
	Status PipelineRunSpecStatus
}

// PipelineRunSpecStatus defines the pipelinerun spec status the user can provide
type PipelineRunSpecStatus string

const (
	// PipelineRunSpecStatusCancelled indicates that the user wants to cancel the task,
	// if not already cancelled or terminated
	PipelineRunSpecStatusCancelled = "PipelineRunCancelled"
)

// PipelineResourceRef can be used to refer to a specific instance of a Resource
type PipelineResourceRef struct {
	// Name of the referent; More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`
	// API version of the referent
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
}

// PipelineRef can be used to refer to a specific instance of a Pipeline.
// Copied from CrossVersionObjectReference: https://github.com/kubernetes/kubernetes/blob/169df7434155cbbc22f1532cba8e0a9588e29ad8/pkg/apis/autoscaling/types.go#L64
type PipelineRef struct {
	// Name of the referent; More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`
	// API version of the referent
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
}

// PipelineTriggerType indicates the mechanism by which this PipelineRun was created.
type PipelineTriggerType string

const (
	// PipelineTriggerTypeManual indicates that this PipelineRun was invoked manually by a user.
	PipelineTriggerTypeManual PipelineTriggerType = "manual"
)

// PipelineTrigger describes what triggered this Pipeline to run. It could be triggered manually,
// or it could have been some kind of external event (not yet designed).
type PipelineTrigger struct {
	Type PipelineTriggerType `json:"type"`
	// +optional
	Name string `json:"name,omitempty"`
}

// PipelineRunStatus defines the observed state of PipelineRun
type PipelineRunStatus struct {
	Conditions duckv1alpha1.Conditions `json:"conditions"`
	// In #107 should be updated to hold the location logs have been uploaded to
	// +optional
	Results *Results `json:"results,omitempty"`
	// map of TaskRun Status with the taskRun name as the key
	//+optional
	TaskRuns map[string]TaskRunStatus `json:"taskRuns,omitempty"`
}

var pipelineRunCondSet = duckv1alpha1.NewBatchConditionSet()

// GetCondition returns the Condition matching the given type.
func (pr *PipelineRunStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return pipelineRunCondSet.Manage(pr).GetCondition(t)
}

// InitializeConditions will set all conditions in pipelineRunCondSet to unknown for the PipelineRun
func (pr *PipelineRunStatus) InitializeConditions() {
	if pr.TaskRuns == nil {
		pr.TaskRuns = make(map[string]TaskRunStatus)
	}
	pipelineRunCondSet.Manage(pr).InitializeConditions()
}

// SetCondition sets the condition, unsetting previous conditions with the same
// type as necessary.
func (pr *PipelineRunStatus) SetCondition(newCond *duckv1alpha1.Condition) {
	if newCond != nil {
		pipelineRunCondSet.Manage(pr).SetCondition(*newCond)
	}
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PipelineRun is the Schema for the pipelineruns API
// +k8s:openapi-gen=true
type PipelineRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec PipelineRunSpec `json:"spec,omitempty"`
	// +optional
	Status PipelineRunStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PipelineRunList contains a list of PipelineRun
type PipelineRunList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PipelineRun `json:"items"`
}

// PipelineTaskRun reports the results of running a step in the Task. Each
// task has the potential to succeed or fail (based on the exit code)
// and produces logs.
type PipelineTaskRun struct {
	Name string `json:"name"`
}

// GetTaskRunRef for pipelinerun
func (pr *PipelineRun) GetTaskRunRef() corev1.ObjectReference {
	return corev1.ObjectReference{
		APIVersion: "build-pipeline.knative.dev/v1alpha1",
		Kind:       "TaskRun",
		Namespace:  pr.Namespace,
		Name:       pr.Name,
	}
}

// SetDefaults for pipelinerun
func (pr *PipelineRun) SetDefaults() {}

// GetPVC gets PVC for
func (pr *PipelineRun) GetPVC() *corev1.PersistentVolumeClaim {
	var pvcSizeBytes int64
	// TODO(shashwathi): make this value configurable
	pvcSizeBytes = 5 * 1024 * 1024 * 1024 // 5 GBs
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       pr.Namespace,
			Name:            pr.GetPVCName(),
			OwnerReferences: pr.GetOwnerReference(),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			// Multiple tasks should be allowed to read
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: *resource.NewQuantity(pvcSizeBytes, resource.BinarySI),
				},
			},
		},
	}
}

// GetOwnerReference gets the pipeline run as owner reference for any related objects
func (pr *PipelineRun) GetOwnerReference() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		*metav1.NewControllerRef(pr, groupVersionKind),
	}
}

// GetPVCName provides name of PVC for corresponding PR
func (pr *PipelineRun) GetPVCName() string {
	return fmt.Sprintf("%s-pvc", pr.Name)
}
