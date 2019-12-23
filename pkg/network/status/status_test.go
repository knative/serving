/*
Copyright 2019 The Knative Authors.

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

package status

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	neturl "net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network/ingress"

	"go.uber.org/zap/zaptest"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/network"
)

var (
	ingTemplate = &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "whatever",
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"foo.bar.com",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}},
		},
	}
)

func TestProbeLifecycle(t *testing.T) {
	ing := ingTemplate.DeepCopy()
	hash, err := ingress.InsertProbe(ing)
	if err != nil {
		t.Fatalf("failed to insert probe: %v", err)
	}

	// Simulate that the latest configuration is not applied yet by returning a different
	// hash once and then the by returning the expected hash.
	hashes := make(chan string, 1)
	hashes <- "not-the-hash-you-are-looking-for"
	go func() {
		for {
			hashes <- hash
		}
	}()

	// Dummy handler returning HTTP 500 (it should never be called during probing)
	dummyRequests := make(chan *http.Request)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dummyRequests <- r
		w.WriteHeader(500)
	})

	// Actual probe handler used in Activator and Queue-Proxy
	probeHandler := network.NewProbeHandler(dummyHandler)

	// Dummy handler keeping track of received requests, mimicking AppendHeader of K-Network-Hash
	// and simulate a non-existing host by returning 404.
	probeRequests := make(chan *http.Request)
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "foo.bar.com") {
			w.WriteHeader(404)
			return
		}

		probeRequests <- r
		r.Header.Set(network.HashHeaderName, <-hashes)
		probeHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewServer(finalHandler)
	defer ts.Close()
	url, err := neturl.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}
	hostname := url.Hostname()
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}

	ready := make(chan *v1alpha1.Ingress)
	prober := NewProber(
		zaptest.NewLogger(t).Sugar(),
		fakeProbeTargetLister{{
			PodIPs: sets.NewString(hostname),
			Port:   strconv.Itoa(port),
			URLs:   []neturl.URL{*url},
		}},
		func(ing *v1alpha1.Ingress) {
			ready <- ing
		})

	prober.stateExpiration = 2 * time.Second
	prober.cleanupPeriod = 500 * time.Millisecond

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	// The first call to IsReady must succeed and return false
	ok, err := prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	const expHostHeader = "foo.bar.com"

	// Wait for the first (failing) and second (success) requests to be executed and validate Host header
	for i := 0; i < 2; i++ {
		req := <-probeRequests
		if req.Host != expHostHeader {
			t.Fatalf("Host header = %q, want %q", req.Host, expHostHeader)
		}
	}

	// Wait for the probing to eventually succeed
	<-ready

	// The subsequent calls to IsReady must succeed and return true
	for i := 0; i < 5; i++ {
		if ok, err = prober.IsReady(context.Background(), ing); err != nil {
			t.Fatalf("IsReady returned unexpected error: %v", err)
		} else if !ok {
			t.Fatalf("IsReady returned %v, want: %v", ok, false)
		}

		time.Sleep(prober.cleanupPeriod)
	}

	select {
	// Wait for the cleanup to happen
	case <-time.After(prober.stateExpiration + prober.cleanupPeriod):
		break
	// Validate that no probe requests were issued (cached)
	case <-probeRequests:
		t.Fatal("an unexpected probe request was received")
	// Validate that no requests went through the probe handler
	case <-dummyRequests:
		t.Fatal("an unexpected request went through the probe handler")
	}

	// The state has expired and been removed
	ok, err = prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first request (success) to be executed
	<-probeRequests

	// Wait for the probing to eventually succeed
	<-ready

	select {
	// Validate that no requests went through the probe handler
	case <-dummyRequests:
		t.Fatal("an unexpected request went through the probe handler")
	default:
		break
	}
}

func TestProbeListerFail(t *testing.T) {
	ing := ingTemplate.DeepCopy()
	ready := make(chan *v1alpha1.Ingress)
	prober := NewProber(
		zaptest.NewLogger(t).Sugar(),
		notFoundLister{},
		func(ing *v1alpha1.Ingress) {
			ready <- ing
		})

	// If we can't list, this  must fail and return false
	ok, err := prober.IsReady(context.Background(), ing)
	if err == nil {
		t.Fatal("IsReady returned unexpected success")
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}
}

func TestCancelPodProbing(t *testing.T) {
	ing := ingTemplate.DeepCopy()
	// Handler keeping track of received requests and mimicking an Ingress not ready
	requests := make(chan *http.Request, 100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.WriteHeader(404)
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()
	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}
	hostname := url.Hostname()
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "gateway",
		},
		Status: v1.PodStatus{
			PodIP: strings.Split(url.Host, ":")[0],
		},
	}

	ready := make(chan *v1alpha1.Ingress)
	prober := NewProber(
		zaptest.NewLogger(t).Sugar(),
		fakeProbeTargetLister{{
			PodIPs: sets.NewString(hostname),
			Port:   strconv.Itoa(port),
			URLs:   []neturl.URL{*url},
		}},
		func(ing *v1alpha1.Ingress) {
			ready <- ing
		})

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	ok, err := prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first probe request
	<-requests

	// Create a new version of the Ingress
	ing = ing.DeepCopy()
	ing.Spec.Rules[0].Hosts[0] = "blabla.net"

	// Check that probing is unsuccessful
	select {
	case <-ready:
		t.Fatal("Probing succeeded while it should not have succeeded")
	default:
	}

	ok, err = prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Drain requests for the old version
	for req := range requests {
		log.Printf("req.Host: %s", req.Host)
		if strings.HasPrefix(req.Host, "blabla.net") {
			break
		}
	}

	// Cancel Pod probing
	prober.CancelPodProbing(pod)

	// Check that the requests were for the new version
	close(requests)
	for req := range requests {
		if !strings.HasPrefix(req.Host, "blabla.net") {
			t.Fatalf("Unexpected Host: want: %s, got: %s", "blabla.net", req.Host)
		}
	}
}

func TestCancelIngresProbing(t *testing.T) {
	ing := ingTemplate.DeepCopy()
	// Handler keeping track of received requests and mimicking an Ingress not ready
	requests := make(chan *http.Request, 100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.WriteHeader(404)
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()
	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}
	hostname := url.Hostname()
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}

	ready := make(chan *v1alpha1.Ingress)
	prober := NewProber(
		zaptest.NewLogger(t).Sugar(),
		fakeProbeTargetLister{{
			PodIPs: sets.NewString(hostname),
			Port:   strconv.Itoa(port),
			URLs:   []neturl.URL{*url},
		}},
		func(ing *v1alpha1.Ingress) {
			ready <- ing
		})

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	ok, err := prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first probe request
	<-requests

	// Create a new version of the Ingress
	ing = ing.DeepCopy()
	ing.Spec.Rules[0].Hosts[0] = "blabla.net"

	// Check that probing is unsuccessful
	select {
	case <-ready:
		t.Fatal("Probing succeeded while it should not have succeeded")
	default:
	}

	ok, err = prober.IsReady(context.Background(), ing)
	if err != nil {
		t.Fatalf("IsReady returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Drain requests for the old version
	for req := range requests {
		log.Printf("req.Host: %s", req.Host)
		if strings.HasPrefix(req.Host, "blabla.net") {
			break
		}
	}

	// Cancel Ingress probing
	prober.CancelIngressProbing(ing)

	// Check that the requests were for the new version
	close(requests)
	for req := range requests {
		if !strings.HasPrefix(req.Host, "blabla.net") {
			t.Fatalf("Unexpected Host: want: %s, got: %s", "blabla.net", req.Host)
		}
	}
}

type fakeProbeTargetLister []ProbeTarget

func (l fakeProbeTargetLister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) ([]ProbeTarget, error) {
	targets := []ProbeTarget{}
	for _, target := range l {
		newTarget := ProbeTarget{
			PodIPs: target.PodIPs,
			Port:   target.Port,
		}
		for _, url := range target.URLs {
			newURL := url
			newURL.Host = ing.Spec.Rules[0].Hosts[0]
			newTarget.URLs = append(newTarget.URLs, newURL)
		}
		targets = append(targets, newTarget)
	}
	return targets, nil
}

type notFoundLister struct{}

func (l notFoundLister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) ([]ProbeTarget, error) {
	return nil, errors.New("not found")
}
