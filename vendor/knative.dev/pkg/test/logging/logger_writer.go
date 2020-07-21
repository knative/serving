/*
Copyright 2020 The Knative Authors

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
package logging

import (
	"bytes"
	"io"
	"strings"
)

type loggerWriter struct {
	prepend string
	logf    FormatLogger

	buf bytes.Buffer
}

func NewLoggerWriter(prepend string, logf FormatLogger) io.Writer {
	return &loggerWriter{
		prepend: prepend,
		logf:    logf,
	}
}

func (l *loggerWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	splitted := strings.Split(s, "\n")

	// Get the remaining of the previous write
	splitted[0] = l.buf.String() + splitted[0]
	l.buf.Reset()

	// Write out the lines
	for i := 0; i < len(splitted)-1; i++ {
		l.logf(l.prepend + splitted[i])
	}

	n, err = l.buf.WriteString(splitted[len(splitted)-1])
	return
}
