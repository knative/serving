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

package configmap

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoad(t *testing.T) {
	want := map[string]string{
		"foo":    "bar",
		"a.b.c":  "blah",
		".z.y.x": "hidden!",
	}
	tmpdir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() = %v", err)
	}
	defer os.RemoveAll(tmpdir)

	// Write out the files as they should be loaded.
	for k, v := range want {
		err := ioutil.WriteFile(path.Join(tmpdir, k), []byte(v), 0644)
		if err != nil {
			t.Fatalf("WriteFile(%q) = %v", k, err)
		}
	}

	got, err := Load(tmpdir)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load (-want, +got) = %v", diff)
	}
}

func TestLoadError(t *testing.T) {
	if data, err := Load("/does/not/exist"); err == nil {
		t.Errorf("Load() = %v, want error", data)
	}
}

func TestReadFileError(t *testing.T) {
	written := map[string]string{
		"foo":    "bar",
		"a.b.c":  "blah",
		".z.y.x": "hidden!",
	}
	tmpdir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir() = %v", err)
	}
	defer os.RemoveAll(tmpdir)

	// Write out the files as write-only, so we fail reading.
	for k, v := range written {
		err := ioutil.WriteFile(path.Join(tmpdir, k), []byte(v), 0200)
		if err != nil {
			t.Fatalf("WriteFile(%q) = %v", k, err)
		}
	}

	if got, err := Load(tmpdir); err == nil {
		t.Fatalf("Load() = %v, want error", got)
	}
}
