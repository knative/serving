package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
)

// ErrValidTracerRequired error
var ErrValidTracerRequired = errors.New("valid tracer required")

// Client holds a Zipkin instrumented HTTP Client.
type Client struct {
	*http.Client
	tracer           *zipkin.Tracer
	httpTrace        bool
	defaultTags      map[string]string
	transportOptions []TransportOption
}

// ClientOption allows optional configuration of Client.
type ClientOption func(*Client)

// WithClient allows one to add a custom configured http.Client to use.
func WithClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client == nil {
			client = &http.Client{}
		}
		c.Client = client
	}
}

// ClientTrace allows one to enable Go's net/http/httptrace.
func ClientTrace(enabled bool) ClientOption {
	return func(c *Client) {
		c.httpTrace = enabled
	}
}

// ClientTags adds default Tags to inject into client application spans.
func ClientTags(tags map[string]string) ClientOption {
	return func(c *Client) {
		c.defaultTags = tags
	}
}

// TransportOptions passes optional Transport configuration to the internal
// transport used by Client.
func TransportOptions(options ...TransportOption) ClientOption {
	return func(c *Client) {
		c.transportOptions = options
	}
}

// NewClient returns an HTTP Client adding Zipkin instrumentation around an
// embedded standard Go http.Client.
func NewClient(tracer *zipkin.Tracer, options ...ClientOption) (*Client, error) {
	if tracer == nil {
		return nil, ErrValidTracerRequired
	}

	c := &Client{tracer: tracer, Client: &http.Client{}}
	for _, option := range options {
		option(c)
	}

	c.transportOptions = append(
		c.transportOptions,
		// the following Client settings override provided transport settings.
		RoundTripper(c.Client.Transport),
		TransportTrace(c.httpTrace),
	)
	transport, err := NewTransport(tracer, c.transportOptions...)
	if err != nil {
		return nil, err
	}
	c.Client.Transport = transport

	return c, nil
}

// DoWithAppSpan wraps http.Client's Do with tracing using an application span.
func (c *Client) DoWithAppSpan(req *http.Request, name string) (res *http.Response, err error) {
	var parentContext model.SpanContext

	if span := zipkin.SpanFromContext(req.Context()); span != nil {
		parentContext = span.Context()
	}

	appSpan := c.tracer.StartSpan(name, zipkin.Parent(parentContext))

	zipkin.TagHTTPMethod.Set(appSpan, req.Method)
	zipkin.TagHTTPPath.Set(appSpan, req.URL.Path)

	res, err = c.Client.Do(
		req.WithContext(zipkin.NewContext(req.Context(), appSpan)),
	)
	if err != nil {
		zipkin.TagError.Set(appSpan, err.Error())
		appSpan.Finish()
		return
	}

	if c.httpTrace {
		appSpan.Annotate(time.Now(), "wr")
	}

	if res.ContentLength > 0 {
		zipkin.TagHTTPResponseSize.Set(appSpan, strconv.FormatInt(res.ContentLength, 10))
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		statusCode := strconv.FormatInt(int64(res.StatusCode), 10)
		zipkin.TagHTTPStatusCode.Set(appSpan, statusCode)
		if res.StatusCode > 399 {
			zipkin.TagError.Set(appSpan, statusCode)
		}
	}

	res.Body = &spanCloser{
		ReadCloser:   res.Body,
		sp:           appSpan,
		traceEnabled: c.httpTrace,
	}
	return
}
