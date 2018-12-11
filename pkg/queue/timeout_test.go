package queue

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeToFirstByteTimeoutHandler(t *testing.T) {
	tests := []struct {
		name           string
		timeout        time.Duration
		handler        func(writeErrors chan error) http.Handler
		timeoutMessage string
		wantStatus     int
		wantBody       string
		wantWriteError bool
	}{{
		name:    "all good",
		timeout: 10 * time.Second,
		handler: func(writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:    "timeout",
		timeout: 50 * time.Millisecond,
		handler: func(writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				_, werr := w.Write([]byte("hi"))
				writeErrors <- werr
			})
		},
		wantStatus:     http.StatusServiceUnavailable,
		wantBody:       defaultTimeoutBody,
		wantWriteError: true,
	}, {
		name:    "write then sleep",
		timeout: 10 * time.Millisecond,
		handler: func(writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				time.Sleep(50 * time.Millisecond)
				w.Write([]byte("hi"))
			})
		},
		wantStatus: http.StatusOK,
		wantBody:   "hi",
	}, {
		name:    "custom timeout message",
		timeout: 50 * time.Millisecond,
		handler: func(writeErrors chan error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				_, werr := w.Write([]byte("hi"))
				writeErrors <- werr
			})
		},
		timeoutMessage: "request timeout",
		wantStatus:     http.StatusServiceUnavailable,
		wantBody:       "request timeout",
		wantWriteError: true,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			writeErrors := make(chan error, 1)
			rr := httptest.NewRecorder()
			handler := TimeToFirstByteTimeoutHandler(test.handler(writeErrors), test.timeout, test.timeoutMessage)

			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != test.wantStatus {
				t.Errorf("Handler returned wrong status code: got %v want %v", status, test.wantStatus)
			}

			if rr.Body.String() != test.wantBody {
				t.Errorf("Handler returned unexpected body: got %q want %q", rr.Body.String(), test.wantBody)
			}

			if test.wantWriteError {
				err := <-writeErrors
				if err != http.ErrHandlerTimeout {
					t.Errorf("Expected a timeout error, got %v", err)
				}
			}
		})
	}
}
