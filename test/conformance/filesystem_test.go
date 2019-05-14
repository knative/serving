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
	"fmt"
	"testing"

	"github.com/knative/serving/test"
	"github.com/knative/serving/test/types"
)

func verifyPermissionsString(resp string, expected string) error {
	if len(resp) != len(expected) {
		return fmt.Errorf("perm = %q (len:%d), want: %q (len:%d)", resp, len(resp), expected, len(expected))
	}

	for index := range expected {
		if expected[index] != '*' && expected[index] != resp[index] {
			return fmt.Errorf("perm[%d] = %c, want: %c", index, expected[index], resp[index])
		}
	}
	return nil
}

func testFiles(t *testing.T, clients *test.Clients, paths map[string]types.FileInfo) error {
	_, ri, err := fetchRuntimeInfo(t, clients, &test.Options{})
	if err != nil {
		return err
	}

	for path, file := range paths {
		riFile, ok := ri.Host.Files[path]
		if !ok {
			return fmt.Errorf("runtime contract file info not present: %s", path)
		}

		if file.Error != "" && file.Error != riFile.Error {
			return fmt.Errorf("%s.Error = %s, want: %s", path, riFile.Error, file.Error)
		}

		if file.IsDir != nil && *file.IsDir != *riFile.IsDir {
			return fmt.Errorf("%s.IsDir = %t, want: %t", path, *riFile.IsDir, *file.IsDir)
		}

		if file.SourceFile != "" && file.SourceFile != riFile.SourceFile {
			return fmt.Errorf("%s.SourceFile = %s, want: %s", path, riFile.SourceFile, file.SourceFile)
		}

		if file.Perm != "" {
			if err := verifyPermissionsString(riFile.Perm, file.Perm); err != nil {
				return fmt.Errorf("%s has invalid permissions string %s: %v", path, riFile.Perm, err)
			}
		}
	}
	return nil
}

// TestMustHaveFiles asserts that the file system has all the MUST have paths and they have appropriate permissions
// and type as applicable.
func TestMustHaveFiles(t *testing.T) {
	t.Parallel()
	clients := setup(t)
	if err := testFiles(t, clients, types.MustFiles); err != nil {
		t.Error(err)
	}
}

// TestShouldHaveFiles asserts that the file system has all the SHOULD have paths and that they have the appropriate
// permissions and type as applicable.
func TestShouldHaveFiles(t *testing.T) {
	t.Parallel()
	clients := setup(t)
	if err := testFiles(t, clients, types.ShouldFiles); err != nil {
		t.Error(err)
	}
}
