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
	"strings"

	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/api/equality"
)

// Validate taskrun
func (tr *TaskRun) Validate() *apis.FieldError {
	if err := validateObjectMetadata(tr.GetObjectMeta()).ViaField("metadata"); err != nil {
		return err
	}
	return tr.Spec.Validate()
}

// Validate taskrun spec
func (ts *TaskRunSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(ts, &TaskRunSpec{}) {
		return apis.ErrMissingField("spec")
	}

	// can't have both taskRef and taskSpec at the same time
	if (ts.TaskRef != nil && ts.TaskRef.Name != "") && ts.TaskSpec != nil {
		return apis.ErrDisallowedFields("spec.taskref", "spec.taskspec")
	}

	// Check that one of TaskRef and TaskSpec is present
	if (ts.TaskRef == nil || (ts.TaskRef != nil && ts.TaskRef.Name == "")) && ts.TaskSpec == nil {
		return apis.ErrMissingField("spec.taskref.name", "spec.taskspec")
	}

	// Check for Trigger
	if err := ts.Trigger.Validate("spec.trigger"); err != nil {
		return err
	}

	// check for input resources
	if err := ts.Inputs.Validate("spec.Inputs"); err != nil {
		return err
	}

	// check for output resources
	if err := ts.Outputs.Validate("spec.Outputs"); err != nil {
		return err
	}

	// check for results
	if ts.Results != nil {
		if err := ts.Results.Validate("spec.results"); err != nil {
			return err
		}
	}
	return nil
}

func (i TaskRunInputs) Validate(path string) *apis.FieldError {
	if err := checkForPipelineResourceDuplicates(i.Resources, fmt.Sprintf("%s.Resources.Name", path)); err != nil {
		return err
	}
	return validateParameters(i.Params)
}

func (o TaskRunOutputs) Validate(path string) *apis.FieldError {
	return checkForPipelineResourceDuplicates(o.Resources, fmt.Sprintf("%s.Resources.Name", path))
}

func checkForPipelineResourceDuplicates(resources []TaskResourceBinding, path string) *apis.FieldError {
	encountered := map[string]struct{}{}
	for _, r := range resources {
		// We should provide only one binding for each resource required by the Task.
		name := strings.ToLower(r.Name)
		if _, ok := encountered[strings.ToLower(name)]; ok {
			return apis.ErrMultipleOneOf(path)
		}
		encountered[name] = struct{}{}
	}
	return nil
}

// Validate validates that the task trigger is of a known type. If it was triggered by a PipelineRun, the
// name of the trigger should be the name of a PipelienRun.
func (r TaskTrigger) Validate(path string) *apis.FieldError {
	if r.Type == "" {
		return nil
	}

	taskType := strings.ToLower(string(r.Type))
	for _, allowed := range []TaskTriggerType{TaskTriggerTypePipelineRun, TaskTriggerTypeManual} {
		allowedType := strings.ToLower(string(allowed))

		if taskType == allowedType {
			if allowedType == strings.ToLower(string(TaskTriggerTypePipelineRun)) && r.Name == "" {
				return apis.ErrMissingField(fmt.Sprintf("%s.name", path))
			}
			return nil
		}
	}
	return apis.ErrInvalidValue(string(r.Type), fmt.Sprintf("%s.type", path))
}

func validateParameters(params []Param) *apis.FieldError {
	// Template must not duplicate parameter names.
	seen := map[string]struct{}{}
	for _, p := range params {
		if _, ok := seen[strings.ToLower(p.Name)]; ok {
			return apis.ErrMultipleOneOf("spec.inputs.params")
		}
		seen[p.Name] = struct{}{}
	}
	return nil
}
