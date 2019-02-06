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
	"bufio"
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

func TestRewinder(t *testing.T) {
	expectStr := func(want, got string, err error) {
		if err != nil {
			t.Fatalf("Expected to read string, got error: %v", err)
		}
		if want != got {
			t.Errorf("Unexpected string, want %q, got %q", want, got)
		}
	}

	cont := make(chan bool)
	rp, wp := io.Pipe() // Note: wp.Write() blocks until rp.Read()

	rewinder := NewRewinder(rp)
	out := bufio.NewReader(rewinder)

	go func() {
		t.Log("Writing chunk #1")
		wp.Write([]byte("s1:"))

		t.Log("Writing chunk #2")
		wp.Write([]byte("s2:"))

		t.Log("Rewinding")
		rewinder.Close()

		cont <- true

		t.Log("Writing chunk #3")
		wp.Write([]byte("s3:"))

		t.Log("Closing stream")
		wp.Close()
	}()

	got, err := out.ReadString(byte(':'))
	t.Log("Checking chunk #1")
	expectStr("s1:", got, err)

	got, err = out.ReadString(byte(':'))
	t.Log("Checking chunk #2")
	expectStr("s2:", got, err)

	<-cont

	gotAll, err := ioutil.ReadAll(out)
	t.Log("Checking chunks #1-3")
	expectStr("s1:s2:s3:", string(gotAll), err)

	t.Log("Rewinding again")
	rewinder.Close()

	gotAll, err = ioutil.ReadAll(out)
	t.Log("Checking chunks #1-3 again")
	expectStr("s1:s2:s3:", string(gotAll), err)
}

func TestRewinder_Cleanup(t *testing.T) {
	want := "foo"

	in := &spyReadCloser{ReadCloser: ioutil.NopCloser(strings.NewReader(want))}
	rewinder := NewRewinder(in)

	got, err := ioutil.ReadAll(rewinder)
	if err != nil {
		t.Fatalf("Expected to read string, got error: %v", err)
	}
	if want != string(got) {
		t.Errorf("Unexpected string, want %q, got %q", want, got)
	}

	rewinder.Close()

	if !in.Closed {
		t.Error("Input ReadCloser not closed")
	}

	ioutil.ReadAll(rewinder)

	if in.ReadAfterClose {
		t.Error("Read() after Close() in input ReadCloser")
	}
}
