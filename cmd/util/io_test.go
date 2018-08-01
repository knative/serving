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
	"bytes"
	"io/ioutil"
	"testing"
)

func TestRewinder(t *testing.T) {
	str := "test string"
	rc := &SpyCloser{Reader: bytes.NewBufferString(str)}
	rewinder := NewRewinder(rc)

	b1, err := ioutil.ReadAll(rewinder)
	if err != nil {
		t.Errorf("Unexpected error reading b1: %v", err)
	}
	rewinder.Close()

	b2, err := ioutil.ReadAll(rewinder)
	if err != nil {
		t.Errorf("Unexpected error reading b2: %v", err)
	}
	rewinder.Close()

	if string(b1) != str {
		t.Errorf("Unexpected str b1. Want %q, got %q", str, b1)
	}

	if string(b2) != str {
		t.Errorf("Unexpected str b2. Want %q, got %q", str, b2)
	}

	if !rc.Closed {
		t.Errorf("Expected ReadCloser to be closed")
	}
}
