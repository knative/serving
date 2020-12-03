/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shell

import (
	"errors"
	"path"
	"runtime"
)

// ErrCantGetCaller is raised when we can't calculate a caller of NewProjectLocation.
var ErrCantGetCaller = errors.New("can't get caller")

// NewProjectLocation creates a ProjectLocation that is used to calculate
// relative paths within the project.
func NewProjectLocation(pathToRoot string) (ProjectLocation, error) {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		return nil, ErrCantGetCaller
	}
	return &callerLocation{
		caller:     filename,
		pathToRoot: pathToRoot,
	}, nil
}

// RootPath return a path to root of the project.
func (c *callerLocation) RootPath() string {
	return path.Join(path.Dir(c.caller), c.pathToRoot)
}

// callerLocation holds a caller Go file, and a relative location to a project
// root directory. This information can be used to calculate relative paths and
// properly source shell scripts.
type callerLocation struct {
	caller     string
	pathToRoot string
}
