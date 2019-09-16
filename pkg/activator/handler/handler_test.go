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

package handler

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/tracing"

	"knative.dev/pkg/test/helpers"

	"github.com/google/go-cmp/cmp"

	. "knative.dev/pkg/logging/testing"
	_ "knative.dev/pkg/system/testing"
	tracingconfig "knative.dev/pkg/tracing/config"
	tracetesting "knative.dev/pkg/tracing/testing"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatornet "knative.dev/serving/pkg/activator/net"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingfake "knative.dev/serving/pkg/client/clientset/versioned/fake"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions"
	servingv1informers "knative.dev/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
	. "knative.dev/pkg/configmap/testing"
)

const (
	wantBody         = "everything good!"
	testNamespace    = "real-namespace"
	testRevName      = "real-name"
	testRevNameOther = "other-name"
)

func TestActivationHandler(t *testing.T) {
	defer ClearAll()

	tests := []struct {
		label             string
		namespace         string
		name              string
		wantBody          string
		wantCode          int
		wantErr           error
		probeErr          error
		probeCode         int
		probeResp         []string
		tryTimeout        time.Duration
		endpointsInformer corev1informers.EndpointsInformer
		sksLister         netlisters.ServerlessServiceLister
		svcLister         corev1listers.ServiceLister
		destsUpdate       activatornet.RevisionDestsUpdate
		reporterCalls     []reporterCall
	}{{
		label:             "active endpoint",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "everything good!",
		wantCode:          http.StatusOK,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000, networking.ServicePortNameHTTP1)),
		destsUpdate: activatornet.RevisionDestsUpdate{
			Rev:   types.NamespacedName{testNamespace, testRevName},
			Dests: dests(1000),
		},
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusOK,
			Attempts:   1,
			Value:      1,
		}},
		tryTimeout: 100 * time.Millisecond,
	}, {
		label:             "no active endpoint",
		namespace:         "fake-namespace",
		name:              "fake-name",
		wantBody:          errMsg("revision.serving.knative.dev \"fake-name\" not found"),
		wantCode:          http.StatusNotFound,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000, networking.ServicePortNameHTTP1)),
		destsUpdate: activatornet.RevisionDestsUpdate{
			Rev:   types.NamespacedName{"fake-namespace", testRevName},
			Dests: sets.NewString(),
		},
		reporterCalls: nil,
		tryTimeout:    100 * time.Millisecond,
	}, {
		label:             "request error",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          "request error\n",
		wantCode:          http.StatusBadGateway,
		wantErr:           errors.New("request error"),
		endpointsInformer: endpointsInformer(endpoints(testNamespace, testRevName, 1000, networking.ServicePortNameHTTP1)),
		destsUpdate: activatornet.RevisionDestsUpdate{
			Rev:   types.NamespacedName{testNamespace, testRevName},
			Dests: dests(1000),
		},
		reporterCalls: []reporterCall{{
			Op:         "ReportRequestCount",
			Namespace:  testNamespace,
			Revision:   testRevName,
			Service:    "service-real-name",
			Config:     "config-real-name",
			StatusCode: http.StatusBadGateway,
			Attempts:   1,
			Value:      1,
		}},
	}, {
		label:             "broken get k8s svc",
		namespace:         testNamespace,
		name:              testRevName,
		wantBody:          context.DeadlineExceeded.Error() + "\n",
		wantCode:          http.StatusServiceUnavailable,
		wantErr:           nil,
		endpointsInformer: endpointsInformer(endpoints("bogus-namespace", testRevName, 1000, networking.ServicePortNameHTTP1)),
		svcLister:         serviceLister(service("bogus-namespace", testRevName, "http")),
		reporterCalls:     nil,
	}}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			probeResponses := make([]activatortest.FakeResponse, len(test.probeResp))
			for i := 0; i < len(test.probeResp); i++ {
				probeResponses[i] = activatortest.FakeResponse{
					Err:  test.probeErr,
					Code: test.probeCode,
					Body: test.probeResp[i],
				}
			}
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:     "test-host",
				ProbeResponses: probeResponses,
				RequestResponse: &activatortest.FakeResponse{
					Err:  test.wantErr,
					Code: test.wantCode,
					Body: test.wantBody,
				},
			}
			rt := network.RoundTripperFunc(fakeRt.RT)
			logger := TestLogger(t)

			reporter := &fakeReporter{}
			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}
			revisions := revisionInformer(revision(testNamespace, testRevName))

			if test.svcLister == nil {
				test.svcLister = serviceLister(service(testNamespace, testRevName, "http"))
			}

			throttler := activatornet.NewThrottler(
				params,
				revisions,
				test.endpointsInformer,
				logger)

			rbmUpdateCh := make(chan activatornet.RevisionDestsUpdate)
			defer close(rbmUpdateCh)
			go throttler.Run(rbmUpdateCh)

			rbmUpdateCh <- test.destsUpdate

			stopCh := make(chan struct{})
			controller.StartInformers(stopCh, revisions.Informer(), test.endpointsInformer.Informer())

			// We want to stop our informers and RBM before we clear RBM.
			defer close(stopCh)

			handler := (New(logger, reporter, throttler,
				revisions.Lister(),
				serviceLister(service(testNamespace, testRevName, "http")),
				sksLister(sks(testNamespace, testRevName)),
			)).(*activationHandler)

			// Setup transports.
			handler.transport = rt

			if test.sksLister != nil {
				handler.sksLister = test.sksLister
			}
			if test.svcLister != nil {
				handler.serviceLister = test.svcLister
			}

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
			req.Header.Set(activator.RevisionHeaderNamespace, test.namespace)
			req.Header.Set(activator.RevisionHeaderName, test.name)
			req.Host = "test-host"

			// Timeout context
			if test.tryTimeout == 0 {
				test.tryTimeout = 200 * time.Millisecond
			}
			tryContext, cancel := context.WithTimeout(context.Background(), test.tryTimeout)
			defer cancel()

			// Set up config store to populate context.
			configStore := setupConfigStore(t)
			ctx := configStore.ToContext(tryContext)

			handler.ServeHTTP(resp, req.WithContext(ctx))

			if resp.Code != test.wantCode {
				t.Errorf("Unexpected response status. Want %d, got %d", test.wantCode, resp.Code)
			}

			gotBody, _ := ioutil.ReadAll(resp.Body)
			if string(gotBody) != test.wantBody {
				t.Errorf("Unexpected response body. Response body %q, want %q", gotBody, test.wantBody)
			}

			// Filter out response time reporter calls
			var gotCalls []reporterCall
			if reporter.calls != nil {
				gotCalls = make([]reporterCall, 0)
				for _, gotCall := range reporter.calls {
					if gotCall.Op != "ReportResponseTime" {
						gotCalls = append(gotCalls, gotCall)
					}
				}
			}

			if diff := cmp.Diff(test.reporterCalls, gotCalls); diff != "" {
				t.Errorf("Reporting calls are different (-want, +got) = %v", diff)
			}

		})
	}
}

func TestActivationHandlerProxyHeader(t *testing.T) {
	breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
	namespace, revName := testNamespace, testRevName
	revisions := revisionInformer(revision(namespace, revName))
	endpoints := endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity, networking.ServicePortNameHTTP1))
	services := serviceLister(service(testNamespace, testRevName, networking.ServicePortNameHTTP1))

	interceptCh := make(chan *http.Request, 1)
	rt := network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		interceptCh <- r
		fake := httptest.NewRecorder()
		return fake.Result(), nil
	})

	updateCh := make(chan activatornet.RevisionDestsUpdate)
	defer close(updateCh)

	stopCh := make(chan struct{})
	defer close(stopCh)
	controller.StartInformers(stopCh, revisions.Informer(), endpoints.Informer())

	throttler := activatornet.NewThrottler(
		breakerParams,
		revisions,
		endpoints,
		TestLogger(t))
	go throttler.Run(updateCh)

	updateCh <- activatornet.RevisionDestsUpdate{
		Rev:           types.NamespacedName{namespace, revName},
		ClusterIPDest: "129.0.0.1:1234",
		Dests:         dests(breakerParams.InitialCapacity),
	}

	handler := &activationHandler{
		transport:      rt,
		logger:         TestLogger(t),
		reporter:       &fakeReporter{},
		throttler:      throttler,
		revisionLister: revisions.Lister(),
		serviceLister:  services,
		sksLister:      sksLister(sks(testNamespace, testRevName)),
	}

	writer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, namespace)
	req.Header.Set(activator.RevisionHeaderName, revName)

	// set up config store to populate context
	configStore := setupConfigStore(t)
	ctx := configStore.ToContext(req.Context())
	handler.ServeHTTP(writer, req.WithContext(ctx))

	select {
	case httpReq := <-interceptCh:
		if got := httpReq.Header.Get(network.ProxyHeaderName); got != activator.Name {
			t.Errorf("Header '%s' does not have the expected value. Want = '%s', got = '%s'.", network.ProxyHeaderName, activator.Name, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for a request to be intercepted")
	}
}

func TestActivationHandlerTraceSpans(t *testing.T) {
	testcases := []struct {
		name         string
		wantSpans    int
		traceBackend tracingconfig.BackendType
	}{{
		name:         "zipkin trace enabled",
		wantSpans:    3,
		traceBackend: tracingconfig.Zipkin,
	}, {
		name:         "trace disabled",
		wantSpans:    0,
		traceBackend: tracingconfig.None,
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup transport
			fakeRt := activatortest.FakeRoundTripper{
				RequestResponse: &activatortest.FakeResponse{
					Err:  nil,
					Code: http.StatusOK,
					Body: wantBody,
				},
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			// Create tracer with reporter recorder
			reporter, co := tracetesting.FakeZipkinExporter()
			defer reporter.Close()
			oct := tracing.NewOpenCensusTracer(co)
			defer oct.Finish()

			cfg := tracingconfig.Config{
				Backend: tc.traceBackend,
				Debug:   true,
			}
			if err := oct.ApplyConfig(&cfg); err != nil {
				t.Errorf("Failed to apply tracer config: %v", err)
			}

			namespace := testNamespace
			revName := testRevName
			revisions := revisionInformer(revision(namespace, revName))
			breakerParams := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}
			endpoints := endpointsInformer(endpoints(namespace, revName, breakerParams.InitialCapacity, networking.ServicePortNameHTTP1))
			services := serviceLister(service(testNamespace, testRevName, "http"))

			updateCh := make(chan activatornet.RevisionDestsUpdate)
			defer close(updateCh)

			stopCh := make(chan struct{})
			defer close(stopCh)
			controller.StartInformers(stopCh, revisions.Informer(), endpoints.Informer())

			throttler := activatornet.NewThrottler(
				breakerParams,
				revisions,
				endpoints,
				TestLogger(t))

			go throttler.Run(updateCh)

			updateCh <- activatornet.RevisionDestsUpdate{
				Rev:           types.NamespacedName{namespace, revName},
				ClusterIPDest: "129.0.0.1:1234",
				Dests:         dests(breakerParams.InitialCapacity),
			}

			handler := &activationHandler{
				transport:      rt,
				logger:         TestLogger(t),
				reporter:       &fakeReporter{},
				throttler:      throttler,
				revisionLister: revisions.Lister(),
				serviceLister:  services,
				sksLister:      sksLister(sks(testNamespace, testRevName)),
			}
			handler.transport = &ochttp.Transport{
				Base: rt,
			}

			// set up config store to populate context
			configStore := setupConfigStore(t)

			_ = sendRequest(namespace, revName, handler, configStore)

			gotSpans := reporter.Flush()
			if len(gotSpans) != tc.wantSpans {
				t.Errorf("Got %d spans, expected %d", len(gotSpans), tc.wantSpans)
			}

			spanNames := []string{"throttler_try", "/", "proxy"}
			for i, spanName := range spanNames[0:tc.wantSpans] {
				if gotSpans[i].Name != spanName {
					t.Errorf("Got span %d named %q, expected %q", i, gotSpans[i].Name, spanName)
				}
			}
		})
	}
}

func sendRequest(namespace, revName string, handler *activationHandler, store *activatorconfig.Store) *httptest.ResponseRecorder {
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	req.Header.Set(activator.RevisionHeaderNamespace, namespace)
	req.Header.Set(activator.RevisionHeaderName, revName)
	ctx := store.ToContext(req.Context())
	handler.ServeHTTP(resp, req.WithContext(ctx))
	return resp
}

type reporterCall struct {
	Op         string
	Namespace  string
	Service    string
	Config     string
	Revision   string
	StatusCode int
	Attempts   int
	Value      int64
	Duration   time.Duration
}

type fakeReporter struct {
	calls []reporterCall
	mux   sync.Mutex
}

func (f *fakeReporter) ReportRequestConcurrency(ns, service, config, rev string, v int64) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:        "ReportRequestConcurrency",
		Namespace: ns,
		Service:   service,
		Config:    config,
		Revision:  rev,
		Value:     v,
	})

	return nil
}

func (f *fakeReporter) ReportRequestCount(ns, service, config, rev string, responseCode, numTries int) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:         "ReportRequestCount",
		Namespace:  ns,
		Service:    service,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Attempts:   numTries,
		Value:      1,
	})

	return nil
}

func (f *fakeReporter) ReportResponseTime(ns, service, config, rev string, responseCode int, d time.Duration) error {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.calls = append(f.calls, reporterCall{
		Op:         "ReportResponseTime",
		Namespace:  ns,
		Service:    service,
		Config:     config,
		Revision:   rev,
		StatusCode: responseCode,
		Duration:   d,
	})

	return nil
}

func revision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: "config-" + testRevName,
				serving.ServiceLabelKey:       "service-" + testRevName,
			},
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(1),
			},
		},
	}
}

func revisionInformer(revs ...*v1alpha1.Revision) servingv1informers.RevisionInformer {
	fake := servingfake.NewSimpleClientset()
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	revisions := informer.Serving().V1alpha1().Revisions()

	for _, rev := range revs {
		fake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}

	return revisions
}

func service(namespace, name string, portName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  name,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name: portName,
				Port: 8080,
			}},
			ClusterIP: "129.0.0.1",
		}}
}

func serviceLister(svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	services := informer.Core().V1().Services()

	for _, svc := range svcs {
		fake.Core().Services(svc.Namespace).Create(svc)
		services.Informer().GetIndexer().Add(svc)
	}

	return services.Lister()
}

func setupConfigStore(t *testing.T) *activatorconfig.Store {
	configStore := activatorconfig.NewStore(TestLogger(t))
	tracingConfig := ConfigMapFromTestFile(t, tracingconfig.ConfigName)
	configStore.OnConfigChanged(tracingConfig)
	return configStore
}

func sks(namespace, name string) *nv1a1.ServerlessService {
	return &nv1a1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: nv1a1.ServerlessServiceStatus{
			// Randomize the test.
			PrivateServiceName: name,
			ServiceName:        helpers.AppendRandomString(name),
		},
	}
}

func sksLister(skss ...*nv1a1.ServerlessService) netlisters.ServerlessServiceLister {
	fake := servingfake.NewSimpleClientset()
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	services := informer.Networking().V1alpha1().ServerlessServices()

	for _, sks := range skss {
		fake.Networking().ServerlessServices(sks.Namespace).Create(sks)
		services.Informer().GetIndexer().Add(sks)
	}

	return services.Lister()
}

func dests(count int) sets.String {
	ret := sets.NewString()
	for i := 1; i <= count; i++ {
		ret.Insert(fmt.Sprintf("127.0.0.%v:1234", i))
	}
	return ret
}

func endpoints(namespace, name string, count int, portName string) *corev1.Endpoints {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				serving.RevisionUID:       "fake-uid",
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  name,
			},
		}}

	epAddresses := []corev1.EndpointAddress{}
	for i := 1; i <= count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i)
		epAddresses = append(epAddresses, corev1.EndpointAddress{IP: ip})
	}
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: epAddresses,
		Ports: []corev1.EndpointPort{{
			Name: portName,
			Port: 1234,
		}},
	}}

	return ep
}

func endpointsInformer(eps ...*corev1.Endpoints) corev1informers.EndpointsInformer {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	endpoints := informer.Core().V1().Endpoints()

	for _, ep := range eps {
		fake.Core().Endpoints(ep.Namespace).Create(ep)
		endpoints.Informer().GetIndexer().Add(ep)
	}

	return endpoints
}

func errMsg(msg string) string {
	return fmt.Sprintf("Error getting active endpoint: %v\n", msg)
}
