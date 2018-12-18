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
	"log"
	"os"
)

const (
	// Filename to store output that acts as input to testgrid.
	// Should be of the form junit_*.xml
	Filename = "junit_knative.xml"
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

// GetArtifactsDir gets the aritfacts directory where we should put the artifacts.
// By default, it will look at the env var ARTIFACTS.
func GetArtifactsDir() string {
	dir := os.Getenv("ARTIFACTS")
	if dir == "" {
		log.Printf("Env variable ARTIFACTS not set. Using './artifacts' instead.")
		return "./artifacts"
	}
	return dir
}

// CreateTestgridXML junit xml file in the default artifacts directory
func CreateTestgridXML(tc []TestCase) error {
	ts := TestSuite{TestCases: tc}
	return CreateXMLOutput(ts, GetArtifactsDir())
}

// CreateXMLOutput creates the junit xml file in the provided artifacts directory
func CreateXMLOutput(ts TestSuite, artifactsDir string) error {
	op, err := xml.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}

	outputFile := artifactsDir + "/" + Filename
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
