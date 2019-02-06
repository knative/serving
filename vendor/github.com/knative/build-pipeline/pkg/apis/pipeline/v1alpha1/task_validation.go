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

package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/knative/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (t *Task) Validate() *apis.FieldError {
	if err := validateObjectMetadata(t.GetObjectMeta()); err != nil {
		return err.ViaField("metadata")
	}
	return t.Spec.Validate()
}

func (ts *TaskSpec) Validate() *apis.FieldError {
	if equality.Semantic.DeepEqual(ts, &TaskSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	// A Task must have a valid BuildSpec.
	if err := ts.GetBuildSpec().Validate(); err != nil {
		return err
	}

	// A task doesn't have to have inputs or outputs, but if it does they must be valid.
	// A task can't duplicate input or output names.

	if ts.Inputs != nil {
		for _, resource := range ts.Inputs.Resources {
			if err := validateResourceType(resource, fmt.Sprintf("taskspec.Inputs.Resources.%s.Type", resource.Name)); err != nil {
				return err
			}
		}
		if err := checkForDuplicates(ts.Inputs.Resources, "taskspec.Inputs.Resources.Name"); err != nil {
			return err
		}
	}
	if ts.Outputs != nil {
		for _, resource := range ts.Outputs.Resources {
			if err := validateResourceType(resource, fmt.Sprintf("taskspec.Outputs.Resources.%s.Type", resource.Name)); err != nil {
				return err
			}
		}
		if err := checkForDuplicates(ts.Outputs.Resources, "taskspec.Outputs.Resources.Name"); err != nil {
			return err
		}
	}

	// Validate task step names
	for _, step := range ts.Steps {
		if errs := validation.IsDNS1123Label(step.Name); len(errs) > 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("invalid value %q", step.Name),
				Paths:   []string{"taskspec.steps.name"},
				Details: "Task step name must be a valid DNS Label, For more info refer to https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names",
			}
		}
	}

	if err := validateInputParameterVariables(ts.Steps, ts.Inputs); err != nil {
		return err
	}
	if err := validateResourceVariables(ts.Steps, ts.Inputs, ts.Outputs); err != nil {
		return err
	}
	return nil
}

func validateInputParameterVariables(steps []corev1.Container, inputs *Inputs) *apis.FieldError {
	parameterNames := map[string]struct{}{}
	if inputs != nil {
		for _, p := range inputs.Params {
			parameterNames[p.Name] = struct{}{}
		}
	}
	return validateVariables(steps, "params", parameterNames)
}

func validateResourceVariables(steps []corev1.Container, inputs *Inputs, outputs *Outputs) *apis.FieldError {
	resourceNames := map[string]struct{}{}
	if inputs != nil {
		for _, r := range inputs.Resources {
			resourceNames[r.Name] = struct{}{}
		}
	}
	if outputs != nil {
		for _, r := range outputs.Resources {
			resourceNames[r.Name] = struct{}{}
		}
	}
	return validateVariables(steps, "resources", resourceNames)
}

func validateVariables(steps []corev1.Container, prefix string, vars map[string]struct{}) *apis.FieldError {
	for _, step := range steps {
		if err := validateVariable("name", step.Name, prefix, vars); err != nil {
			return err
		}
		if err := validateVariable("image", step.Image, prefix, vars); err != nil {
			return err
		}
		if err := validateVariable("workingDir", step.WorkingDir, prefix, vars); err != nil {
			return err
		}
		for i, cmd := range step.Command {
			if err := validateVariable(fmt.Sprintf("command[%d]", i), cmd, prefix, vars); err != nil {
				return err
			}
		}
		for i, arg := range step.Args {
			if err := validateVariable(fmt.Sprintf("arg[%d]", i), arg, prefix, vars); err != nil {
				return err
			}
		}
		for _, env := range step.Env {
			if err := validateVariable(fmt.Sprintf("env[%s]", env.Name), env.Value, prefix, vars); err != nil {
				return err
			}
		}
		for i, v := range step.VolumeMounts {
			if err := validateVariable(fmt.Sprintf("volumeMount[%d].Name", i), v.Name, prefix, vars); err != nil {
				return err
			}
			if err := validateVariable(fmt.Sprintf("volumeMount[%d].MountPath", i), v.MountPath, prefix, vars); err != nil {
				return err
			}
			if err := validateVariable(fmt.Sprintf("volumeMount[%d].SubPath", i), v.SubPath, prefix, vars); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateVariable(name, value, prefix string, vars map[string]struct{}) *apis.FieldError {
	if v, present := extractVariable(value, prefix); present {
		if _, ok := vars[v]; !ok {
			return &apis.FieldError{
				Message: fmt.Sprintf("non-existent variable in %q for step %s", value, name),
				Paths:   []string{"taskspec.steps." + name},
			}
		}
	}
	return nil
}

func extractVariable(s, prefix string) (string, bool) {
	re := regexp.MustCompile("\\${(?:inputs|outputs)\\." + prefix + "\\.(.*)}")
	if re.MatchString(s) {
		// ${inputs.resources.foo} with prefix=resources -> [${inputs.resources.foo foo}]
		// ${inputs.resources.foo.bar} with prefix=resources -> [${inputs.resources.foo foo.bar}]
		v := re.FindStringSubmatch(s)[1]
		// foo -> foo
		// foo.bar -> foo
		// foo.bar.baz -> foo
		return strings.SplitN(v, ".", 2)[0], true
	}
	return "", false
}

func checkForDuplicates(resources []TaskResource, path string) *apis.FieldError {
	encountered := map[string]struct{}{}
	for _, r := range resources {
		if _, ok := encountered[strings.ToLower(r.Name)]; ok {
			return apis.ErrMultipleOneOf(path)
		}
		encountered[strings.ToLower(r.Name)] = struct{}{}
	}
	return nil
}

func validateResourceType(r TaskResource, path string) *apis.FieldError {
	for _, allowed := range AllResourceTypes {
		if r.Type == allowed {
			return nil
		}
	}
	return apis.ErrInvalidValue(string(r.Type), path)
}
