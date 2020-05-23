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

// testgrid.go provides methods to perform action on testgrid.

package testgrid

import (
	"log"
	"os"
	"path"

	"knative.dev/pkg/test/helpers"
	"knative.dev/pkg/test/junit"
	"knative.dev/pkg/test/prow"
)

const (
	filePrefix = "junit_"
	extension  = ".xml"
)

// CreateXMLOutput creates the junit xml file in the provided artifacts directory
func CreateXMLOutput(tc []junit.TestCase, testName string) error {
	ts := junit.TestSuites{}
	ts.AddTestSuite(&junit.TestSuite{Name: testName, TestCases: tc})

	// ensure artifactsDir exist, in case not invoked from this script
	artifactsDir := prow.GetLocalArtifactsDir()
	if err := helpers.CreateDir(artifactsDir); err != nil {
		return err
	}
	op, err := ts.ToBytes("", "  ")
	if err != nil {
		return err
	}

	outputFile := path.Join(artifactsDir, filePrefix+testName+extension)
	log.Printf("Storing output in %s", outputFile)
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(string(op) + "\n"); err != nil {
		return err
	}
	return nil
}
