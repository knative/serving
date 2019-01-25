// +build e2e

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

package conformance

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
)

func verifyPermString(resp string, expected string) error {
	if len(resp) != len(expected) {
		return fmt.Errorf("Length of expected and response string don't match. Expected Response: '%s' Received Response: '%s' Expected length: %d Received length: %d",
			expected, resp, len(expected), len(resp))
	}

	for index := range expected {
		if string(expected[index]) != "*" && expected[index] != resp[index] {
			return fmt.Errorf("Permission strings don't match at Index : %d. Expected : '%s' Received Response : '%s'",
				index, expected, resp)
		}
	}

	return nil
}

func TestMustFileSystemPermissions(t *testing.T) {
	logger := logging.GetContextLogger("TestFileSystemPermissions")
	clients := setup(t)
	for key, value := range MustFilePathSpecs {
		resp, _, err := fetchEnvInfo(clients, logger, test.EnvImageFilePathInfoPath+"?"+test.EnvImageFilePathQueryParam+"="+key, &test.Options{})
		if err != nil {
			t.Fatal(err)
		}

		var f FilePathInfo
		err = json.Unmarshal(resp, &f)
		if err != nil {
			t.Fatalf("Error unmarshalling response: %v", err)
		}

		if f.IsDirectory != value.IsDirectory {
			t.Fatalf("%s isDirectory mismatch. Expect : %t Received : %t", key, value.IsDirectory, f.IsDirectory)
		}

		if err = verifyPermString(f.PermString[1:], value.PermString); err != nil {
			t.Fatal(err)
		}
	}
}
