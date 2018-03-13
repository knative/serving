package http

import (
	"net/http"
	"strconv"
	"sync/atomic"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
)

type handler struct {
	tracer          *zipkin.Tracer
	name            string
	next            http.Handler
	tagResponseSize bool
	defaultTags     map[string]string
}

// ServerOption allows Middleware to be optionally configured.
type ServerOption func(*handler)

// ServerTags adds default Tags to inject into server spans.
func ServerTags(tags map[string]string) ServerOption {
	return func(h *handler) {
		h.defaultTags = tags
	}
}

// TagResponseSize will instruct the middleware to Tag the http response size
// in the server side span.
func TagResponseSize(enabled bool) ServerOption {
	return func(h *handler) {
		h.tagResponseSize = enabled
	}
}

// SpanName sets the name of the spans the middleware creates. Use this if
// wrapping each endpoint with its own Middleware.
// If omitting the SpanName option, the middleware will use the http request
// method as span name.
func SpanName(name string) ServerOption {
	return func(h *handler) {
		h.name = name
	}
}

// NewServerMiddleware returns a http.Handler middleware with Zipkin tracing.
func NewServerMiddleware(t *zipkin.Tracer, options ...ServerOption) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := &handler{
			tracer: t,
			next:   next,
		}
		for _, option := range options {
			option(h)
		}
		return h
	}
}

// ServeHTTP implements http.Handler.
func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var spanName string

	// try to extract B3 Headers from upstream
	sc := h.tracer.Extract(b3.ExtractHTTP(r))

	remoteEndpoint, _ := zipkin.NewEndpoint("", r.RemoteAddr)

	if len(h.name) == 0 {
		spanName = r.Method
	} else {
		spanName = h.name
	}

	// create Span using SpanContext if found
	sp := h.tracer.StartSpan(
		spanName,
		zipkin.Kind(model.Server),
		zipkin.Parent(sc),
		zipkin.RemoteEndpoint(remoteEndpoint),
	)

	for k, v := range h.defaultTags {
		sp.Tag(k, v)
	}

	// add our span to context
	ctx := zipkin.NewContext(r.Context(), sp)

	// tag typical HTTP request items
	zipkin.TagHTTPMethod.Set(sp, r.Method)
	zipkin.TagHTTPUrl.Set(sp, r.URL.String())
	zipkin.TagHTTPRequestSize.Set(sp, strconv.FormatInt(r.ContentLength, 10))

	// create http.ResponseWriter interceptor for tracking response size and
	// status code.
	ri := &rwInterceptor{w: w, statusCode: 200}

	// tag found response size and status code on exit
	defer func() {
		code := ri.getStatusCode()
		sCode := strconv.Itoa(code)
		if code > 399 {
			zipkin.TagError.Set(sp, sCode)
		}
		zipkin.TagHTTPStatusCode.Set(sp, sCode)
		if h.tagResponseSize {
			zipkin.TagHTTPResponseSize.Set(sp, ri.getResponseSize())
		}
		sp.Finish()
	}()

	// call next http Handler func using our updated context.
	h.next.ServeHTTP(ri, r.WithContext(ctx))
}

// rwInterceptor intercepts the ResponseWriter so it can track response size
// and returned status code.
type rwInterceptor struct {
	w          http.ResponseWriter
	size       uint64
	statusCode int
}

func (r *rwInterceptor) Header() http.Header {
	return r.w.Header()
}

func (r *rwInterceptor) Write(b []byte) (n int, err error) {
	n, err = r.w.Write(b)
	atomic.AddUint64(&r.size, uint64(n))
	return
}

func (r *rwInterceptor) WriteHeader(i int) {
	r.statusCode = i
	r.w.WriteHeader(i)
}

func (r *rwInterceptor) getStatusCode() int {
	return r.statusCode
}

func (r *rwInterceptor) getResponseSize() string {
	return strconv.FormatUint(atomic.LoadUint64(&r.size), 10)
}
