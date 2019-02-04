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
	"strconv"
	"strings"

	"github.com/knative/test-infra/tools/webhook-apicoverage/coveragecalculator"
)

// GetJSONTypeDisplay is a helper method to display API Coverage details in json-like format.
func GetJSONTypeDisplay(coverageData []coveragecalculator.TypeCoverage, displayRules DisplayRules) string {
	var buffer strings.Builder
	for _, typeCoverage := range coverageData {
		packageName := typeCoverage.Package
		if displayRules.PackageNameRule != nil {
			packageName = displayRules.PackageNameRule(packageName)
		}

		typeName := typeCoverage.Type
		if displayRules.TypeNameRule != nil {
			typeName = displayRules.TypeNameRule(typeName)
		}

		buffer.WriteString("Package: " + packageName + "\n")
		buffer.WriteString("Type: " + typeName + "\n")
		buffer.WriteString("\n{\n")

		for _, fieldCoverage := range typeCoverage.Fields {
			fieldDisplay := defaultTypeDisplay(fieldCoverage)
			if displayRules.FieldRule != nil {
				fieldDisplay = displayRules.FieldRule(fieldCoverage)
			}
			buffer.WriteString(fieldDisplay)
		}

		buffer.WriteString("}\n\n")
	}

	return buffer.String()
}

func defaultTypeDisplay(field *coveragecalculator.FieldCoverage) string {
	var buffer strings.Builder
	buffer.WriteString("\t" + field.Field)
	if field.Ignored {
		buffer.WriteString("\tIgnored")
	} else {
		buffer.WriteString("\tCovered: " + strconv.FormatBool(field.Coverage))
		if len(field.Values) > 0 {
			buffer.WriteString("\tValues: [" + strings.Join(field.Values, ",") + "]")
		}
	}
	buffer.WriteString("\n")
	return buffer.String()
}