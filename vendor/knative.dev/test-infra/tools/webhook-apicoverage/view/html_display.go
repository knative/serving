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

	"knative.dev/test-infra/tools/webhook-apicoverage/coveragecalculator"
)

// HTMLHeader sets up an HTML page with the right style format
var HTMLHeader = fmt.Sprintf(`
<!DOCTYPE html>
<html>
<style type="text/css">
<!--

.tab { margin-left: 50px; }

.styleheader {color: white; size: A4}

.covered {color: green; size: A3}

.notcovered {color: red; size: A3}

.ignored {color: white; size: A4}

.values {color: yellow; size: A3}

table, th, td { border: 1px solid white; text-align: center}

.braces {color: white; size: A3}
-->
</style>
<body style="background-color:rgb(0,0,0); font-family: Arial">
`)

// HTMLFooter closes the tags for the HTML page.
var HTMLFooter = fmt.Sprintf(`
    </body>
  </html>
`)

// GetHTMLDisplay is a helper method to display API Coverage details in json-like format inside a HTML page.
func GetHTMLDisplay(coverageData []coveragecalculator.TypeCoverage, displayRules DisplayRules) string {
	var buffer strings.Builder
	buffer.WriteString(HTMLHeader)
	for _, typeCoverage := range coverageData {
		packageName := typeCoverage.Package
		if displayRules.PackageNameRule != nil {
			packageName = displayRules.PackageNameRule(packageName)
		}

		typeName := typeCoverage.Type
		if displayRules.TypeNameRule != nil {
			typeName = displayRules.TypeNameRule(typeName)
		}
		buffer.WriteString(fmt.Sprint(`<div class="styleheader">`))
		buffer.WriteString(fmt.Sprintf(`<br>Package: %s`, packageName))
		buffer.WriteString(fmt.Sprintf(`<br>Type: %s`, typeName))
		buffer.WriteString(fmt.Sprint(`<br></div>`))
		buffer.WriteString(fmt.Sprint(`<div class="braces"><br>{</div>`))
		for _, fieldCoverage := range typeCoverage.Fields {
			var fieldDisplay string
			if displayRules.FieldRule != nil {
				fieldDisplay = displayRules.FieldRule(fieldCoverage)
			} else {
				fieldDisplay = defaultHTMLTypeDisplay(fieldCoverage)
			}
			buffer.WriteString(fieldDisplay)
		}

		buffer.WriteString(fmt.Sprint(`<div class="braces">}</div>`))
		buffer.WriteString(fmt.Sprint(`<br>`))

	}

	buffer.WriteString(HTMLFooter)
	return buffer.String()
}

func defaultHTMLTypeDisplay(field *coveragecalculator.FieldCoverage) string {
	var buffer strings.Builder
	if field.Ignored {
		buffer.WriteString(fmt.Sprintf(`<div class="ignored tab">%s</div>`, field.Field))
	} else if field.Coverage {
		buffer.WriteString(fmt.Sprintf(`<div class="covered tab">%s`, field.Field))
		if len(field.Values) > 0 && !strings.Contains(strings.ToLower(field.Field), "uid") {
			buffer.WriteString(fmt.Sprintf(`&emsp; &emsp; <span class="values">Values: [%s]</span>`, strings.Join(field.GetValues(), ", ")))
		}
		buffer.WriteString(fmt.Sprint(`</div>`))
	} else {
		buffer.WriteString(fmt.Sprintf(`<div class="notcovered tab">%s</div>`, field.Field))
	}
	return buffer.String()
}

// GetHTMLCoverageValuesDisplay is a helper method to display coverage values inside a HTML table.
func GetHTMLCoverageValuesDisplay(coverageValues *coveragecalculator.CoverageValues) string {
	var buffer strings.Builder
	buffer.WriteString(fmt.Sprint(`<br>`))
	buffer.WriteString(fmt.Sprint(`<div class="styleheader">Coverage Values</div>`))
	buffer.WriteString(fmt.Sprint(`<br>`))
	buffer.WriteString(fmt.Sprint(`  <table style="width: 30%">`))
	buffer.WriteString(fmt.Sprintf(`<tr class="styleheader"><td>Total Fields</td><td>%d</td></tr>`, coverageValues.TotalFields))
	buffer.WriteString(fmt.Sprintf(`<tr class="styleheader"><td>Covered Fields</td><td>%d</td></tr>`, coverageValues.CoveredFields))
	buffer.WriteString(fmt.Sprintf(`<tr class="styleheader"><td>Ignored Fields</td><td>%d</td></tr>`, coverageValues.IgnoredFields))

	percentCoverage := 0.0
	if coverageValues.TotalFields > 0 {
		percentCoverage = (float64(coverageValues.CoveredFields) / float64(coverageValues.TotalFields-coverageValues.IgnoredFields)) * 100
	}
	buffer.WriteString(fmt.Sprintf(`<tr class="styleheader"><td>Coverage Percentage</td><td>%f</td></tr>`, percentCoverage))
	buffer.WriteString(fmt.Sprint(`</table>`))
	return buffer.String()
}
