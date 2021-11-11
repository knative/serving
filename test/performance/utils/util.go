//go:build performance
// +build performance

/*
Copyright 2021 The Knative Authors

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
	"html/template"
	"io/ioutil"
	"os"
)

func GenerateHTMLFile(sourceCSV string, targetHTML string) error {
	data, err := ioutil.ReadFile(sourceCSV)
	if err != nil {
		return fmt.Errorf("failed to read csv file %s", err)
	}
	htmlTemplate, err := Asset("templates/single_chart.html")
	if err != nil {
		return fmt.Errorf("failed to load asset: %s", err)
	}
	viewTemplate, err := template.New("chart").Parse(string(htmlTemplate))
	if err != nil {
		return fmt.Errorf("failed to parse html template %s", err)
	}
	htmlFile, err := os.OpenFile(targetHTML, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open html file %s", err)
	}
	defer htmlFile.Close()
	return viewTemplate.Execute(htmlFile, map[string]interface{}{
		"Data": string(data),
	})
}
