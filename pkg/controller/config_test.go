/*
Copyright 2018 Google LLC.

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

package controller

import (
	"io/ioutil"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoadNonExistentYamlFile(t *testing.T) {
	_, err := LoadConfig("/i/do/not/exist")
	if err == nil {
		t.Error("Expect an error when config file not found")
	}
}

func TestLoadBadYamlFile(t *testing.T) {
	// Create a temp file to container bad YAML.
	f, err := ioutil.TempFile("", "bad-yaml-file")
	defer syscall.Unlink(f.Name())
	ioutil.WriteFile(f.Name(), []byte("bad-yaml-data"), 0600)

	// Parse and verify failure.
	_, err = LoadConfig(f.Name())
	if err == nil {
		t.Error("Expect an error when config file not in right format")
	}
}

func TestLoadGoodYamlFile(t *testing.T) {
	expected := Config{
		DomainSuffix: "foo.bar.net",
	}

	// Create a temp file to contain good YAML.
	f, err := ioutil.TempFile("", "good-yaml-file")
	defer syscall.Unlink(f.Name())
	ioutil.WriteFile(f.Name(), []byte("domainSuffix: foo.bar.net"), 0600)

	// Parse and verify.
	c, err := LoadConfig(f.Name())
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
	}
	if diff := cmp.Diff(expected, *c); diff != "" {
		t.Errorf("expected config  != parsed config (-want +got): %v", diff)
	}
}
