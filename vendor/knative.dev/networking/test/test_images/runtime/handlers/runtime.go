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
	"log"
	"net/http"

	"knative.dev/networking/test/types"
)

var fileAccessExclusions = []string{
	"/dev/tty",
	"/dev/console",
}

func runtimeHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Retrieving Runtime Information")
	w.Header().Set("Content-Type", "application/json")

	filePaths := make([]string, len(types.MustFiles)+len(types.ShouldFiles))
	i := 0
	for key := range types.MustFiles {
		filePaths[i] = key
		i++
	}
	for key := range types.ShouldFiles {
		filePaths[i] = key
		i++
	}

	k := &types.RuntimeInfo{
		Request: requestInfo(r),
		Host: &types.HostInfo{EnvVars: env(),
			Files:      fileInfo(filePaths...),
			FileAccess: fileAccessAttempt(excludeFilePaths(filePaths, fileAccessExclusions)...),
			Cgroups:    cgroups(cgroupPaths...),
			Mounts:     mounts(),
			Stdin:      stdin(),
			User:       userInfo(),
			Args:       args(),
		},
	}

	writeJSON(w, k)
}

func excludeFilePaths(filePaths []string, excludedPaths []string) []string {
	var paths []string
	for _, path := range filePaths {
		excluded := false
		for _, excludedPath := range excludedPaths {
			if path == excludedPath {
				excluded = true
				break
			}
		}

		if !excluded {
			paths = append(paths, path)
		}
	}
	return paths
}
