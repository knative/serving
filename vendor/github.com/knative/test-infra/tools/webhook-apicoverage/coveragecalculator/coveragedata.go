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

package coveragecalculator

// FieldCoverage represents coverage data for a field.
type FieldCoverage struct {
	Field    string          `json:"Field"`
	Values   map[string]bool `json:"Values"`
	Coverage bool            `json:"Covered"`
	Ignored  bool            `json:"Ignored"`
}

// Merge operation merges the field coverage data when multiple nodes represent the same type. (e.g. ConnectedNodes traversal)
func (f *FieldCoverage) Merge(coverage bool, values map[string]bool) {
	if coverage {
		f.Coverage = coverage
		for key, value := range values {
			if _, ok := f.Values[key]; !ok {
				f.Values[key] = value
			}
		}
	}
}

// GetValues returns Values as slice
func (f *FieldCoverage) GetValues() []string {
	values := []string{}
	for key := range f.Values {
		values = append(values, key)
	}
	return values
}

// TypeCoverage encapsulates type information and field coverage.
type TypeCoverage struct {
	Package string                    `json:"Package"`
	Type    string                    `json:"Type"`
	Fields  map[string]*FieldCoverage `json:"Fields"`
}
