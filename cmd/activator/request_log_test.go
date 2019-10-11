/*
Copyright 2019 The Knative Authors

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

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"

	testing2 "knative.dev/pkg/logging/testing"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "knative.dev/serving/pkg/http"

	corev1 "k8s.io/api/core/v1"
)

const (
	testRevisionName  = "testRevision"
	testNamespaceName = "testNs"
	testServiceName   = "testSvc"
	testConfigName    = "testConfig"
)

func TestUpdateRequestLogFromConfigMap(t *testing.T) {
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	buf := bytes.NewBufferString("")
	handler, err := pkghttp.NewRequestLogHandler(baseHandler, buf, "",
		requestLogTemplateInputGetter(revisionLister(t, true)), false /*enableProbeRequestLog*/)
	if err != nil {
		t.Fatalf("want: no error, got: %v", err)
	}

	tests := []struct {
		name     string
		url      string
		body     string
		template string
		want     string
	}{{
		name:     "empty template",
		url:      "http://example.com/testpage",
		body:     "test",
		template: "",
		want:     "",
	}, {
		name:     "template with new line",
		url:      "http://example.com/testpage",
		body:     "test",
		template: "{{.Request.URL}}\n",
		want:     "http://example.com/testpage\n",
	}, {
		name:     "invalid template",
		url:      "http://example.com",
		body:     "test",
		template: "{{}}",
		want:     "http://example.com\n",
	}, {
		name:     "revision info",
		url:      "http://example.com",
		body:     "test",
		template: "{{.Revision.Name}}, {{.Revision.Namespace}}, {{.Revision.Service}}, {{.Revision.Configuration}}, {{.Revision.PodName}}, {{.Revision.PodIP}}",
		want:     "testRevision, testNs, testSvc, testConfig, , \n",
	}, {
		name:     "empty template 2",
		url:      "http://example.com/testpage",
		body:     "test",
		template: "",
		want:     "",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf.Reset()
			cm := &corev1.ConfigMap{}
			cm.Data = map[string]string{"logging.request-log-template": test.template}
			(updateRequestLogFromConfigMap(testing2.TestLogger(t), handler))(cm)
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, test.url, bytes.NewBufferString(test.body))
			req.Header = map[string][]string{
				activator.RevisionHeaderName:      {testRevisionName},
				activator.RevisionHeaderNamespace: {testNamespaceName},
			}
			handler.ServeHTTP(resp, req)

			got := buf.String()
			if got != test.want {
				t.Errorf("got '%v', want '%v'", got, test.want)
			}
		})
	}
}

func TestRequestLogTemplateInputGetter(t *testing.T) {
	tests := []struct {
		name     string
		getter   pkghttp.RequestLogTemplateInputGetter
		request  *http.Request
		response *pkghttp.RequestLogResponse
		want     pkghttp.RequestLogRevision
	}{{
		name:   "success",
		getter: requestLogTemplateInputGetter(revisionLister(t, true)),
		request: &http.Request{Header: map[string][]string{
			activator.RevisionHeaderName:      {testRevisionName},
			activator.RevisionHeaderNamespace: {testNamespaceName},
		}},
		response: &pkghttp.RequestLogResponse{Code: http.StatusAlreadyReported},
		want: pkghttp.RequestLogRevision{
			Namespace:     testNamespaceName,
			Name:          testRevisionName,
			Configuration: testConfigName,
			Service:       testServiceName,
		},
	}, {
		name:   "revision not found",
		getter: requestLogTemplateInputGetter(revisionLister(t, true)),
		request: &http.Request{Header: map[string][]string{
			activator.RevisionHeaderName:      {"foo"},
			activator.RevisionHeaderNamespace: {"bar"},
		}},
		response: &pkghttp.RequestLogResponse{Code: http.StatusAlreadyReported},
		want: pkghttp.RequestLogRevision{
			Name:      "foo",
			Namespace: "bar",
		},
	}, {
		name:   "labels not found",
		getter: requestLogTemplateInputGetter(revisionLister(t, false)),
		request: &http.Request{Header: map[string][]string{
			activator.RevisionHeaderName:      {testRevisionName},
			activator.RevisionHeaderNamespace: {testNamespaceName},
		}},
		response: &pkghttp.RequestLogResponse{Code: http.StatusAlreadyReported},
		want: pkghttp.RequestLogRevision{
			Namespace: testNamespaceName,
			Name:      testRevisionName,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.getter(test.request, test.response)
			if !cmp.Equal(*got.Revision, test.want) {
				t.Errorf("Got = %v, want: %v, diff: %s", got.Revision, test.want, cmp.Diff(got.Revision, test.want))
			}
			if got.Request != test.request {
				t.Errorf("Got = %v, want: %v", got.Request, test.request)
			}
			if got.Response != test.response {
				t.Errorf("Got = %v, want: %v", got.Response, test.response)
			}
		})
	}
}

func revisionLister(t *testing.T, addLabels bool) servinglisters.RevisionLister {
	rev := &v1alpha1.Revision{}
	rev.Name = testRevisionName
	rev.Namespace = testNamespaceName
	if addLabels {
		rev.Labels = map[string]string{
			serving.ConfigurationLabelKey: testConfigName,
			serving.ServiceLabelKey:       testServiceName,
		}
	}

	ctx, _ := rtesting.SetupFakeContext(t)
	fakeservingclient.Get(ctx).ServingV1alpha1().Revisions(testNamespaceName).Create(rev)
	ri := fakerevisioninformer.Get(ctx)
	ri.Informer().GetIndexer().Add(rev)
	return ri.Lister()
}
