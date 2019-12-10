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

package v1

import (
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/test/logging"
)

var camelCaseRegex = regexp.MustCompile(`^[[:upper:]].*`)
var camelCaseSingleWordRegex = regexp.MustCompile(`^[[:upper:]][^[\s]]+$`)

func ValidateCondition(t *logging.TLogger, c *apis.Condition) {
	if c == nil {
		return
	}
	if c.Type == "" {
		t.Error("A Condition.Type must not be an empty string")
	} else if !camelCaseRegex.MatchString(string(c.Type)) {
		t.Error("A Condition.Type must be CamelCase, so must start with an upper-case letter")
	}
	if c.Status != corev1.ConditionTrue && c.Status != corev1.ConditionFalse && c.Status != corev1.ConditionUnknown {
		t.Error("A Condition.Status must be True, False, or Unknown")
	}
	if c.Reason != "" && !camelCaseRegex.MatchString(c.Reason) {
		t.Error("A Condition.Reason, if given, must be a single-word CamelCase")
	}
	if c.Severity != apis.ConditionSeverityError && c.Severity != apis.ConditionSeverityWarning && c.Severity != apis.ConditionSeverityInfo {
		t.Error("A Condition.Status must be '', Warning, or Info")
	}
}
