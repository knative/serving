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
package activator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type Matcher int

const (
	Exact    Matcher = 0
	Contains Matcher = 1
)

type testData struct {
	probe        *v1.Probe
	want         result
	errorMatcher Matcher
}

type result struct {
	ready bool
	err   error
}

func (r result) String() string {
	var errorString string
	if r.err != nil {
		errorString = r.err.Error()
	}
	return fmt.Sprintf("result(ready: %t, err: %s)", r.ready, errorString)
}

func (r result) Match(other result, matcher Matcher) bool {
	if r.ready != other.ready {
		return false
	}
	if (r.err == nil) && (other.err != nil) {
		return false
	}
	if (r.err != nil) && (other.err == nil) {
		return false
	}
	if r.err == nil && other.err == nil {
		return true
	}

	switch matcher {
	case Exact:
		return r.err.Error() == other.err.Error()
	case Contains:
		return strings.Contains(r.err.Error(), other.err.Error())
	default:
		return false
	}
}

func TestCheckHttpGetReadiness(t *testing.T) {
	server := getTestHttpServer(t)
	defer server.Close()

	url, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("error parsing test server url(%s): %s", server.URL, err.Error())
	}

	logger := zap.NewNop().Sugar()

	testCases := generateHttpGetTestCases(t, url)
	for testName, testData := range testCases {
		ready, err := checkHttpGetProbe(testData.probe, logger)
		got := result{ready, err}

		if !got.Match(testData.want, testData.errorMatcher) {
			t.Fatalf("%s. want: %v. got: %v", testName, testData.want, got)
		}
	}
}

func TestCheckHttpGetReadiness_RequestInitialDelay(t *testing.T) {
	var requestTimestamp time.Time

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			requestTimestamp = time.Now()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	url, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("error parsing test server url(%s): %s", server.URL, err.Error())
	}

	probe := getTestHttpGetProbe(t, url)
	probe.InitialDelaySeconds = 2

	revision := &v1alpha1.Revision{
		Spec: v1alpha1.RevisionSpec{
			Container: v1.Container{
				ReadinessProbe: probe,
			},
		},
	}

	urlPort, err := strconv.ParseInt(url.Port(), 10, 32)
	if err != nil {
		t.Fatalf("error parsing port(%s) : %s", url.Port(), err.Error())
	}

	endpoint := Endpoint{
		FQDN: url.Hostname(),
		Port: int32(urlPort),
	}

	invocationTimestamp := time.Now()
	verifyRevisionRoutability(revision, &endpoint, zap.NewNop().Sugar())

	if !endpoint.IsVerified() {
		t.Fatalf("expected endpoint to be verified")
	}

	gotInitialDelay := requestTimestamp.Sub(invocationTimestamp)
	wantInitialDelay := time.Second * time.Duration(int64(probe.InitialDelaySeconds))
	if gotInitialDelay < wantInitialDelay {
		t.Fatalf("less than expected initial delay. got: %v, want: %v", gotInitialDelay, wantInitialDelay)
	}
}

func TestCheckHttpGetReadiness_RequestRetryInterval(t *testing.T) {
	requestRetryCount := 0
	expectedRetryCount := 2

	var requestTimestamps []time.Time

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			requestRetryCount++
			requestTimestamps = append(requestTimestamps, time.Now())
			if requestRetryCount == expectedRetryCount {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	url, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("error parsing test server url(%s): %s", server.URL, err.Error())
	}

	probe := getTestHttpGetProbe(t, url)
	probe.PeriodSeconds = 2

	revision := &v1alpha1.Revision{
		Spec: v1alpha1.RevisionSpec{
			Container: v1.Container{
				ReadinessProbe: probe,
			},
		},
	}

	urlPort, err := strconv.ParseInt(url.Port(), 10, 32)
	if err != nil {
		t.Fatalf("error parsing port(%s) : %s", url.Port(), err.Error())
	}

	endpoint := Endpoint{
		FQDN: url.Hostname(),
		Port: int32(urlPort),
	}

	verifyRevisionRoutability(revision, &endpoint, zap.NewNop().Sugar())
	if !endpoint.IsVerified() {
		t.Fatalf("expected endpoint to be verified")
	}

	if requestRetryCount != expectedRetryCount {
		t.Fatalf("retry count mismatch. got: %d. want: %d", requestRetryCount, expectedRetryCount)
	}

	gotRetryDuration := requestTimestamps[1].Sub(requestTimestamps[0])
	wantRetryDuration := time.Duration(int64(probe.PeriodSeconds))
	if gotRetryDuration < wantRetryDuration {
		t.Fatalf("less than expected duration. got: %#v. want: %#v", gotRetryDuration, wantRetryDuration)
	}
}

func TestCheckHttpGetReadiness_ClientTimesOut(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			responseDelayDuration := 2 * time.Second
			time.Sleep(responseDelayDuration)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	url, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("error parsing test server url(%s): %s", server.URL, err.Error())
	}

	probe := getTestHttpGetProbe(t, url)
	probe.TimeoutSeconds = 1
	ready, err := checkHttpGetProbe(probe, zap.NewNop().Sugar())

	got := result{ready, err}
	want := result{false, errors.New("request canceled")}
	if !got.Match(want, Contains) {
		t.Fatalf("health server request times out. want: %v. got: %v", want, got)
	}
}

func generateHttpGetTestCases(t *testing.T, url *url.URL) (testCases map[string]testData) {
	t.Helper()

	testCases = make(map[string]testData)

	error := errors.New("probe cannot be nil")
	testCases["probe is nil"] = testData{nil, result{false, error}, Exact}

	error = errors.New("probe HTTPGet cannot be nil")
	testCases["probe HTTPGet is nil"] = testData{&v1.Probe{}, result{false, error}, Exact}

	probe := getTestHttpGetProbe(t, url)
	testCases["probe is not nil"] = testData{probe, result{true, nil}, Exact}

	badProbe := probe.DeepCopy()
	badProbe.HTTPGet.Host = "bad_host_name"
	error = errors.New("no such host")
	testCases["probe with bad host name"] = testData{badProbe, result{false, error}, Contains}

	badProbe = probe.DeepCopy()
	badProbe.HTTPGet.Path = "bad_host_path"
	testCases["probe with bad host path"] = testData{badProbe, result{false, nil}, Exact}

	return testCases
}

func getTestHttpServer(t *testing.T) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return httptest.NewServer(handler)
}

func getTestHttpGetProbe(t *testing.T, url *url.URL) *v1.Probe {
	t.Helper()

	return &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Host:   url.Hostname(),
				Path:   "/health",
				Scheme: v1.URISchemeHTTP,
				Port:   getProbePort(t, url),
			},
		},
	}
}

func getProbePort(t *testing.T, url *url.URL) (probePort intstr.IntOrString) {
	t.Helper()

	urlPort, err := strconv.ParseInt(url.Port(), 10, 32)
	if err != nil {
		t.Fatalf("error parsing port(%s) : %s", url.Port(), err.Error())
	}
	probePort = intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: int32(urlPort),
	}
	return probePort
}
