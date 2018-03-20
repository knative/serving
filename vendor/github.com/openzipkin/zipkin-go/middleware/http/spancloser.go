package http

import (
	"io"
	"time"

	zipkin "github.com/openzipkin/zipkin-go"
)

type spanCloser struct {
	io.ReadCloser
	sp           zipkin.Span
	traceEnabled bool
}

func (s *spanCloser) Close() (err error) {
	if s.traceEnabled {
		s.sp.Annotate(time.Now(), "Body Close")
	}
	err = s.ReadCloser.Close()
	s.sp.Finish()
	return
}
