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

package util

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/knative/serving/pkg/ko"
)

const _CommitIDFile = "HEAD"

var (
	commitIDRE = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

// GetGitHubShortCommitID tries to fetch the first 7 digitals of GitHub commit
// ID from HEAD file in KO_DATA_PATH. If it fails, it returns the error it gets.
func GetGitHubShortCommitID() (string, error) {
	data, err := ko.ReadFileFromKoData(_CommitIDFile)
	if err != nil {
		return "", err
	}
	return _ExtractShortCommitID(data)
}

func _ExtractShortCommitID(data string) (string, error) {
	commitID := strings.TrimSpace(data)
	if !commitIDRE.MatchString(commitID) {
		err := fmt.Errorf("%q is not a valid GitHub commit ID", commitID)
		return "", err
	}
	return string(commitID[0:7]), nil
}
