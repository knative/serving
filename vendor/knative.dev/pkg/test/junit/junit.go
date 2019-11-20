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

// junit.go defines types and functions specific to manipulating junit test result XML files

package junit

import (
	"encoding/xml"
	"fmt"
)

// TestStatusEnum is a enum for test result status
type TestStatusEnum string

const (
	// Failed means junit test failed
	Failed TestStatusEnum = "failed"
	// Skipped means junit test skipped
	Skipped TestStatusEnum = "skipped"
	// Passed means junit test passed
	Passed TestStatusEnum = "passed"
)

// TestSuites holds a <testSuites/> list of TestSuite results
type TestSuites struct {
	XMLName xml.Name    `xml:"testsuites"`
	Suites  []TestSuite `xml:"testsuite"`
}

// TestSuite holds <testSuite/> results
type TestSuite struct {
	XMLName    xml.Name       `xml:"testsuite"`
	Name       string         `xml:"name,attr"`
	Time       float64        `xml:"time,attr"` // Seconds
	Failures   int            `xml:"failures,attr"`
	Tests      int            `xml:"tests,attr"`
	TestCases  []TestCase     `xml:"testcase"`
	Properties TestProperties `xml:"properties"`
}

// TestCase holds <testcase/> results
type TestCase struct {
	Name       string         `xml:"name,attr"`
	Time       float64        `xml:"time,attr"` // Seconds
	ClassName  string         `xml:"classname,attr"`
	Failure    *string        `xml:"failure,omitempty"`
	Output     *string        `xml:"system-out,omitempty"`
	Error      *string        `xml:"system-err,omitempty"`
	Skipped    *string        `xml:"skipped,omitempty"`
	Properties TestProperties `xml:"properties"`
}

// TestProperties is an array of test properties
type TestProperties struct {
	Properties []TestProperty `xml:"property"`
}

// TestProperty defines a property of the test
type TestProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// GetTestStatus returns the test status as a string
func (testCase *TestCase) GetTestStatus() TestStatusEnum {
	testStatus := Passed
	switch {
	case testCase.Failure != nil:
		testStatus = Failed
	case testCase.Skipped != nil:
		testStatus = Skipped
	}
	return testStatus
}

// AddProperty adds property to testcase
func (testCase *TestCase) AddProperty(name, val string) {
	property := TestProperty{Name: name, Value: val}
	testCase.Properties.Properties = append(testCase.Properties.Properties, property)
}

// AddTestCase adds a testcase to the testsuite
func (ts *TestSuite) AddTestCase(tc TestCase) {
	ts.TestCases = append(ts.TestCases, tc)
}

// GetTestSuite gets TestSuite struct by name
func (testSuites *TestSuites) GetTestSuite(suiteName string) (*TestSuite, error) {
	for _, testSuite := range testSuites.Suites {
		if testSuite.Name == suiteName {
			return &testSuite, nil
		}
	}
	return nil, fmt.Errorf("Test suite '%s' not found", suiteName)
}

// AddTestSuite adds TestSuite to TestSuites
func (testSuites *TestSuites) AddTestSuite(testSuite *TestSuite) error {
	if _, err := testSuites.GetTestSuite(testSuite.Name); err == nil {
		return fmt.Errorf("Test suite '%s' already exists", testSuite.Name)
	}
	testSuites.Suites = append(testSuites.Suites, *testSuite)
	return nil
}

// ToBytes converts TestSuites struct to bytes array
func (testSuites *TestSuites) ToBytes(prefix, indent string) ([]byte, error) {
	return xml.MarshalIndent(testSuites, prefix, indent)
}

// UnMarshal converts bytes array to TestSuites struct,
// it works with both TestSuites and TestSuite structs, if
// input is a TestSuite struct it will still return a TestSuites
// struct, which is an empty wrapper TestSuites containing only
// the input Suite
func UnMarshal(buf []byte) (*TestSuites, error) {
	var testSuites TestSuites
	if err := xml.Unmarshal(buf, &testSuites); err == nil {
		return &testSuites, nil
	}

	// The input might be a TestSuite if reach here, try parsing with TestSuite
	testSuites.Suites = append([]TestSuite(nil), TestSuite{})
	if err := xml.Unmarshal(buf, &testSuites.Suites[0]); err != nil {
		return nil, err
	}
	return &testSuites, nil
}
