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
	"bytes"
	"io"
)

// NewPrefixer creates a new prefixer that forwards all calls to Write() to
// writer.Write() with all lines prefixed with the value of prefix. Having a
// function instead of a static prefix allows to print timestamps or other
// changing information.
func NewPrefixer(writer io.Writer, prefix func() string) io.Writer {
	return &prefixer{prefix: prefix, writer: writer, trailingNewline: true}
}

type prefixer struct {
	prefix          func() string
	writer          io.Writer
	trailingNewline bool
	buf             bytes.Buffer // reuse buffer to save allocations
}

func (pf *prefixer) Write(payload []byte) (int, error) {
	pf.buf.Reset() // clear the buffer

	for _, b := range payload {
		if pf.trailingNewline {
			pf.buf.WriteString(pf.prefix())
			pf.trailingNewline = false
		}

		pf.buf.WriteByte(b)

		if b == '\n' {
			// do not print the prefix right after the newline character as this might
			// be the very last character of the stream and we want to avoid a trailing prefix.
			pf.trailingNewline = true
		}
	}

	n, err := pf.writer.Write(pf.buf.Bytes())
	if err != nil {
		// never return more than original length to satisfy io.Writer interface
		if n > len(payload) {
			n = len(payload)
		}
		return n, err
	}

	// return original length to satisfy io.Writer interface
	return len(payload), nil
}
