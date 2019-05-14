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
	"os"
	"strings"

	"github.com/knative/serving/test/types"
)

func fileInfo(paths ...string) map[string]types.FileInfo {
	files := map[string]types.FileInfo{}
	for _, path := range paths {
		file, err := os.Stat(path)
		if err != nil {
			files[path] = types.FileInfo{Error: err.Error()}
			continue
		}
		size := file.Size()
		dir := file.IsDir()
		source, _ := os.Readlink(path)

		// If we apply the UNIX permissions mask via 'Perm' the leading
		// character will always be "-" because all the mode bits are dropped.
		perm := strings.TrimPrefix(file.Mode().Perm().String(), "-")

		files[path] = types.FileInfo{
			Size:       &size,
			Perm:       perm,
			ModTime:    file.ModTime(),
			SourceFile: source,
			IsDir:      &dir}
	}
	return files
}
