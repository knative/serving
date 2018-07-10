/*
Copyright 2017 The Knative Authors
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

	"k8s.io/apimachinery/pkg/api/equality"
)

func (rt *Route) Validate() *FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *RouteSpec) Validate() *FieldError {
	if equality.Semantic.DeepEqual(rs, &RouteSpec{}) {
		return errMissingField(currentField)
	}

	// Where a named traffic target points
	type namedTarget struct {
		r string // revision name
		c string // config name
		i int    // index of first occurrence
	}

	// Track the targets of named TrafficTarget entries (to detect duplicates).
	trafficMap := make(map[string]namedTarget)

	percentSum := 0
	for i, tt := range rs.Traffic {
		if err := tt.Validate(); err != nil {
			return err.ViaField(fmt.Sprintf("traffic[%d]", i))
		}
		percentSum += tt.Percent

		if tt.Name == "" {
			// No Name field, so skip the uniqueness check.
			continue
		}
		nt := namedTarget{
			r: tt.RevisionName,
			c: tt.ConfigurationName,
			i: i,
		}
		if ent, ok := trafficMap[tt.Name]; !ok {
			// No entry exists, so add ours
			trafficMap[tt.Name] = nt
		} else if ent.r != nt.r || ent.c != nt.c {
			return &FieldError{
				Message: fmt.Sprintf("Multiple definitions for %q", tt.Name),
				Paths: []string{
					fmt.Sprintf("traffic[%d].name", ent.i),
					fmt.Sprintf("traffic[%d].name", nt.i),
				},
			}
		}
	}

	if percentSum != 100 {
		return &FieldError{
			Message: fmt.Sprintf("Traffic targets sum to %d, want 100", percentSum),
			Paths:   []string{"traffic"},
		}
	}
	return nil
}

func (tt *TrafficTarget) Validate() *FieldError {
	switch {
	case tt.RevisionName != "" && tt.ConfigurationName != "":
		return &FieldError{
			Message: "Expected exactly one, got both",
			Paths:   []string{"revisionName", "configurationName"},
		}
	case tt.RevisionName != "":
	case tt.ConfigurationName != "":
		// These are fine.
	default:
		return &FieldError{
			Message: "Expected exactly one, got neither",
			Paths:   []string{"revisionName", "configurationName"},
		}
	}
	if tt.Percent < 0 || tt.Percent > 100 {
		return errInvalidValue(fmt.Sprintf("%d", tt.Percent), "percent")
	}
	return nil
}
