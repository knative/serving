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

// testgrid.go provides methods to perform action on testgrid.

package testgrid

import (
	"fmt"
	"log"
	"os"
	"path"

	"github.com/knative/test-infra/shared/junit"
	"github.com/knative/test-infra/shared/prow"
)

const (
	filePrefix = "junit_"
	extension  = ".xml"
	// BaseURL is Knative testgrid base URL
	BaseURL = "https://testgrid.knative.dev"
)

// jobNameTestgridURLMap contains harded coded mapping of job name: Testgrid tab URL relative to base URL
var jobNameTestgridURLMap = map[string]string{
	"ci-knative-serving-continuous": "knative-serving#continuous",
}

// createDir creates dir if does not exist.
func createDir(dirPath string) error {
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		if err = os.MkdirAll(dirPath, 0777); err != nil {
			return fmt.Errorf("Failed to create directory: %v", err)
		}
	}
	return nil
}

// GetTestgridTabURL gets Testgrid URL for giving job and filters for Testgrid
func GetTestgridTabURL(jobName string, filters []string) (string, error) {
	url, ok := jobNameTestgridURLMap[jobName]
	if !ok {
		return "", fmt.Errorf("cannot find Testgrid tab for job '%s'", jobName)
	}
	for _, filter := range filters {
		url += "&" + filter
	}
	return fmt.Sprintf("%s/%s", BaseURL, url), nil
}

// CreateXMLOutput creates the junit xml file in the provided artifacts directory
func CreateXMLOutput(tc []junit.TestCase, testName string) error {
	ts := junit.TestSuites{}
	ts.AddTestSuite(&junit.TestSuite{Name: testName, TestCases: tc})

	// ensure artifactsDir exist, in case not invoked from this script
	artifactsDir := prow.GetLocalArtifactsDir()
	if err := createDir(artifactsDir); nil != err {
		return err
	}
	op, err := ts.ToBytes("", "  ")
	if err != nil {
		return err
	}

	outputFile := path.Join(artifactsDir, filePrefix+testName+extension)
	log.Printf("Storing output in %s", outputFile)
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	if err != nil {
		return err
	}
	if _, err := f.WriteString(string(op) + "\n"); err != nil {
		return err
	}
	return nil
}
