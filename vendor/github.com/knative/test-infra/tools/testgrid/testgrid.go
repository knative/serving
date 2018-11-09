/*
Copyright 2018 The Knative Authors

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
	"encoding/xml"
	"os"
)

// TestProperty defines a property of the test
type TestProperty struct {
	Name  string  `xml:"name,attr"`
	Value float32 `xml:"value,attr"`
}

// TestProperties is an array of test properties
type TestProperties struct {
	Property []TestProperty `xml:"property"`
}

// TestCase defines a test case that was executed
type TestCase struct {
	ClassName  string         `xml:"class_name,attr"`
	Name       string         `xml:"name,attr"`
	Time       int            `xml:"time,attr"`
	Properties TestProperties `xml:"properties"`
	Fail       bool           `xml:"failure,omitempty"`
}

// TestSuite defines the set of relevant test cases
type TestSuite struct {
	XMLName   xml.Name   `xml:"testsuite"`
	TestCases []TestCase `xml:"testcase"`
}

// CreateXMLOutput creates the junit xml file in the provided artifacts directory
func CreateXMLOutput(ts TestSuite, artifactsDir string) error {
	op, err := xml.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}

	outputFile := artifactsDir + "/junit_bazel.xml"
	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(string(op) + "\n"); err != nil {
		return err
	}
	return nil
}
