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
	"io"
	"io/ioutil"
	"sync"
)

type rewinder struct {
	sync.Mutex
	rc io.ReadCloser
	rs io.ReadSeeker
}

// rewinder wraps a single-use `ReadCloser` into a `ReadCloser` that can be read multiple times
func NewRewinder(rc io.ReadCloser) io.ReadCloser {
	return &rewinder{rc: rc}
}

func (r *rewinder) Read(b []byte) (int, error) {
	r.Lock()
	defer r.Unlock()
	// On the first `Read()`, the contents of `rc` is read into a buffer `rs`.
	// This buffer is used for all subsequent reads
	if r.rs == nil {
		buf, err := ioutil.ReadAll(r.rc)
		if err != nil {
			return 0, err
		}
		r.rc.Close()

		r.rs = bytes.NewReader(buf)
	}

	return r.rs.Read(b)
}

func (r *rewinder) Close() error {
	r.Lock()
	defer r.Unlock()
	// Rewind the buffer on `Close()` for the next call to `Read`
	r.rs.Seek(0, io.SeekStart)

	return nil
}
