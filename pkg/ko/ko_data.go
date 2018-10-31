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

package ko

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
)

const _KoDataPathEnvName = "KO_DATA_PATH"

var (
	commitIDRE = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

// ReadFileFromKoData tries to read data as string from the file with given name
// under KO_DATA_PATH then returns the content as string. The file is expected
// to be wrapped into the container from /kodata by ko. If it fails, returns
// the error it gets.
func ReadFileFromKoData(filename string) (string, error) {
	koDataPath := os.Getenv(_KoDataPathEnvName)
	if koDataPath == "" {
		err := fmt.Errorf("%q does not exist or is empty", _KoDataPathEnvName)
		return "", err
	}
	fullFilename := strings.Replace(koDataPath+"/"+filename, "//", "/", -1)
	data, err := ioutil.ReadFile(fullFilename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
