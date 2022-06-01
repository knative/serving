package http

import (
	"net/http"
)

var (
	_ http.Flusher        = (*DelayedWriter)(nil)
	_ http.ResponseWriter = (*DelayedWriter)(nil)
)

// DelayedWriter is an implementation of http.ResponseWriter. It delays writing to the output until RealFlush is called.
type DelayedWriter struct {
	ResponseCode int

	writer         http.ResponseWriter
	headerRecorded bool

	buffer        []byte
	bytesBuffered int
}

// NewDelayedWriter creates an http.ResponseWriter that delays writing.
func NewDelayedWriter(w http.ResponseWriter) *DelayedWriter {
	return &DelayedWriter{
		writer: w,
	}
}

// Flush does nothing.
func (dw *DelayedWriter) Flush() {}

// RealFlush flushes the buffer (both header and body) to the client.
func (dw *DelayedWriter) RealFlush() {
	dw.writer.WriteHeader(dw.ResponseCode)
	_, err := dw.writer.Write(dw.buffer[:dw.bytesBuffered])
	if err != nil {
		panic(err)
	}
	dw.writer.(http.Flusher).Flush()
}

// Header returns the header map that will be sent by WriteHeader.
func (dw *DelayedWriter) Header() http.Header {
	return dw.writer.Header()
}

// Write only writes the data to the buffer, not the output.
func (dw *DelayedWriter) Write(p []byte) (int, error) {
	if len(dw.buffer) == 0 {
		dw.buffer = make([]byte, 32*1024)
		dw.bytesBuffered = 0
	}
	bytesCopied := copy(dw.buffer[dw.bytesBuffered:], p)
	dw.bytesBuffered += bytesCopied
	return bytesCopied, nil
}

// WriteHeader only records the response code rather than send the HTTP response header.
func (dw *DelayedWriter) WriteHeader(code int) {
	if dw.headerRecorded {
		return
	}

	dw.headerRecorded = true
	dw.ResponseCode = code
}
