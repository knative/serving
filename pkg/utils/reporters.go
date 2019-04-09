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

package utils

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/kmp"
)

// FieldDiffReporter implements the cmp.Reporter interface. It keeps
// track of the field names that differ between two structs and reports
// them through the FieldDiff() function.
type FieldDiffReporter struct {
	path       cmp.Path
	fieldNames []string
}

// PushStep implements the cmp.Reporter.
func (r *FieldDiffReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

func (r *FieldDiffReporter) currentName() string {
	name := r.path.Last().String()
	return strings.ToLower(strings.TrimPrefix(name, "."))
}

// Report implements the cmp.Reporter.
func (r *FieldDiffReporter) Report(rs cmp.Result) {
	if rs.Equal() {
		return
	}
	r.fieldNames = append(r.fieldNames, r.currentName())
}

// PopStep implements cmp.Reporter.
func (r *FieldDiffReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

// FieldDiff returns the field names that differed between the two
// objects after calling cmp.Equal with the FieldDiffReporter.
func (r *FieldDiffReporter) FieldDiff() []string {
	return r.fieldNames
}

// FieldDiff returns a list of field names that differ between
// x and y.
func FieldDiff(x, y interface{}) []string {
	r := new(FieldDiffReporter)
	kmp.SafeEqual(x, y, cmp.Reporter(r))
	return r.FieldDiff()
}

// ImmutableReporter implements the cmp.Reporter interface. It reports
// on fields which have diffing values.
type ImmutableReporter struct {
	path  cmp.Path
	diffs []string
}

// PushStep implements the cmp.Reporter.
func (r *ImmutableReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

// Report implements the cmp.Reporter.
func (r *ImmutableReporter) Report(rs cmp.Result) {
	if rs.Equal() {
		return
	}
	vx, vy := r.path.Last().Values()
	t := r.path.Last().Type()
	var diff string
	// Prefix struct values with the types to add clarity in output
	if t.Kind() == reflect.Struct {
		diff = fmt.Sprintf("%#v:\n\t-: %+v: \"%+v\"\n\t+: %+v: \"%+v\"\n", r.path, t, vx, t, vy)
	} else {
		diff = fmt.Sprintf("%#v:\n\t-: \"%+v\"\n\t+: \"%+v\"\n", r.path, vx, vy)
	}
	r.diffs = append(r.diffs, diff)
}

// PopStep implements the cmp.Reporter.
func (r *ImmutableReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

// Diff returns a zero-context diff of fields that have changed between
// two objects. cmp.Equal should  be called before this method.
func (r *ImmutableReporter) Diff() string {
	return strings.Join(r.diffs, "")
}
