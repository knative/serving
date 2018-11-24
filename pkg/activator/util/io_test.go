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

import "io"

type spyReadCloser struct {
	io.ReadCloser
	Closed         bool
	ReadAfterClose bool
}

func (s *spyReadCloser) Read(b []byte) (n int, err error) {
	if s.Closed {
		s.ReadAfterClose = true
	}

	return s.ReadCloser.Read(b)
}

func (s *spyReadCloser) Close() error {
	s.Closed = true

	return s.ReadCloser.Close()
}
