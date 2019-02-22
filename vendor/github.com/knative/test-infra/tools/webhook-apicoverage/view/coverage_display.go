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

package view

import (
	"fmt"
	"strings"

	"github.com/knative/test-infra/tools/webhook-apicoverage/coveragecalculator"
)

// GetCoverageValuesDisplay builds the string display for coveragecalculator.CoverageValues
func GetCoverageValuesDisplay(coverageValues *coveragecalculator.CoverageValues) string {
	var buffer strings.Builder
	buffer.WriteString("\nCoverage Values:\n")
	buffer.WriteString(fmt.Sprintf("Total Fields : %d\n", coverageValues.TotalFields))
	buffer.WriteString(fmt.Sprintf("Covered Fields : %d\n", coverageValues.CoveredFields))
	buffer.WriteString(fmt.Sprintf("Ignored Fields : %d\n", coverageValues.IgnoredFields))

	percentCoverage := 0.0
	if coverageValues.CoveredFields > 0 {
		percentCoverage = (float64(coverageValues.CoveredFields) / float64(coverageValues.TotalFields - coverageValues.IgnoredFields)) * 100
	}
	buffer.WriteString(fmt.Sprintf("Coverage Percentage : %f\n", percentCoverage))
	return buffer.String()
}
