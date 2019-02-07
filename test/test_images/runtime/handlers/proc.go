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

package handlers

import (
	"io"
	"os"
	"strconv"

	"github.com/knative/serving/test/types"
)

// stdin attempts to read bytes from the stdin file descriptor and returns the result.
func stdin() *types.Stdin {
	file, err := os.Open("/proc/" + strconv.Itoa(os.Getpid()) + "/fd/0")
	if err != nil {
		return &types.Stdin{Error: err.Error()}
	}

	_, err = file.Read(make([]byte, 1))
	if err == io.EOF {
		return &types.Stdin{EOF: &yes}
	}
	if err != nil {
		return &types.Stdin{Error: err.Error()}
	}

	return &types.Stdin{EOF: &no}
}
