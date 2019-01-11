package http

import (
	"net/http"
	"net/http/httptrace"
	"strconv"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
)

type transport struct {
	tracer      *zipkin.Tracer
	rt          http.RoundTripper
	httpTrace   bool
	defaultTags map[string]string
}

// TransportOption allows one to configure optional transport configuration.
type TransportOption func(*transport)

// RoundTripper adds the Transport RoundTripper to wrap.
func RoundTripper(rt http.RoundTripper) TransportOption {
	return func(t *transport) {
		if rt != nil {
			t.rt = rt
		}
	}
}

// TransportTags adds default Tags to inject into transport spans.
func TransportTags(tags map[string]string) TransportOption {
	return func(t *transport) {
		t.defaultTags = tags
	}
}

// TransportTrace allows one to enable Go's net/http/httptrace.
func TransportTrace(enable bool) TransportOption {
	return func(t *transport) {
		t.httpTrace = enable
	}
}

// NewTransport returns a new Zipkin instrumented http RoundTripper which can be
// used with a standard library http Client.
func NewTransport(tracer *zipkin.Tracer, options ...TransportOption) (http.RoundTripper, error) {
	if tracer == nil {
		return nil, ErrValidTracerRequired
	}

	t := &transport{
		tracer:    tracer,
		rt:        http.DefaultTransport,
		httpTrace: false,
	}

	for _, option := range options {
		option(t)
	}

	return t, nil
}

// RoundTrip satisfies the RoundTripper interface.
func (t *transport) RoundTrip(req *http.Request) (res *http.Response, err error) {
	sp, _ := t.tracer.StartSpanFromContext(
		req.Context(), req.URL.Scheme+"/"+req.Method, zipkin.Kind(model.Client),
	)

	for k, v := range t.defaultTags {
		sp.Tag(k, v)
	}

	if t.httpTrace {
		sptr := spanTrace{
			Span: sp,
		}
		sptr.c = &httptrace.ClientTrace{
			GetConn:              sptr.getConn,
			GotConn:              sptr.gotConn,
			PutIdleConn:          sptr.putIdleConn,
			GotFirstResponseByte: sptr.gotFirstResponseByte,
			Got100Continue:       sptr.got100Continue,
			DNSStart:             sptr.dnsStart,
			DNSDone:              sptr.dnsDone,
			ConnectStart:         sptr.connectStart,
			ConnectDone:          sptr.connectDone,
			TLSHandshakeStart:    sptr.tlsHandshakeStart,
			TLSHandshakeDone:     sptr.tlsHandshakeDone,
			WroteHeaders:         sptr.wroteHeaders,
			Wait100Continue:      sptr.wait100Continue,
			WroteRequest:         sptr.wroteRequest,
		}

		req = req.WithContext(
			httptrace.WithClientTrace(req.Context(), sptr.c),
		)
	}

	zipkin.TagHTTPMethod.Set(sp, req.Method)
	zipkin.TagHTTPPath.Set(sp, req.URL.Path)

	_ = b3.InjectHTTP(req)(sp.Context())

	res, err = t.rt.RoundTrip(req)

	if err != nil {
		zipkin.TagError.Set(sp, err.Error())
		sp.Finish()
		return
	}

	if res.ContentLength > 0 {
		zipkin.TagHTTPResponseSize.Set(sp, strconv.FormatInt(res.ContentLength, 10))
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		statusCode := strconv.FormatInt(int64(res.StatusCode), 10)
		zipkin.TagHTTPStatusCode.Set(sp, statusCode)
		if res.StatusCode > 399 {
			zipkin.TagError.Set(sp, statusCode)
		}
	}
	sp.Finish()
	return
}
