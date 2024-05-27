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

	"knative.dev/networking/test/types"
)

type permissionBits uint32

const (
	otherRead permissionBits = 1 << (2 - iota)
	otherWrite
)

func (p permissionBits) hasPermission(mode permissionBits) bool {
	return p&mode == mode
}

func fileAccessAttempt(filePaths ...string) map[string]types.FileAccessInfo {
	files := map[string]types.FileAccessInfo{}
	for _, path := range filePaths {
		accessInfo := types.FileAccessInfo{}

		if err := checkReadable(path); err != nil {
			accessInfo.ReadErr = err.Error()
		}

		if err := checkWritable(path); err != nil {
			accessInfo.WriteErr = err.Error()
		}

		files[path] = accessInfo
	}
	return files
}

// checkReadable function checks whether path file or directory is readable
func checkReadable(path string) error {
	file, err := os.Stat(path) // It should only return error only if file does not exist
	if err != nil {
		return err
	}

	// We aren't expected to be able to read, so just exit
	perm := permissionBits(file.Mode().Perm())
	if !perm.hasPermission(otherRead) {
		return nil
	}

	if file.IsDir() {
		_, err := os.ReadDir(path)
		return err
	}

	readFile, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	return readFile.Close()
}

// checkWritable function checks whether path file or directory is writable
func checkWritable(path string) error {
	file, err := os.Stat(path) // It should only return error only if file does not exist
	if err != nil {
		return err
	}

	// We aren't expected to be able to write, so just exits
	perm := permissionBits(file.Mode().Perm())
	if !perm.hasPermission(otherWrite) {
		return nil
	}

	if file.IsDir() {
		writeFile, err := os.CreateTemp(path, "random")
		if writeFile != nil {
			os.Remove(writeFile.Name())
		}
		return err
	}

	writeFile, err := os.OpenFile(path, os.O_APPEND, 0)
	if writeFile != nil {
		defer writeFile.Close()
	}
	return err
}
