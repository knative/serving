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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ltesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator"
	activatorhandler "knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	pkghttp "knative.dev/serving/pkg/http"
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
		requestLogTemplateInputGetter, false /*enableProbeRequestLog*/)
	if err != nil {
		t.Fatal("want: no error, got:", err)
	}

	tests := []struct {
		name string
		url  string
		data map[string]string
		want string
	}{{
		name: "empty template",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "",
		},
	}, {
		name: "template with new line",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "{{.Request.URL}}\n",
			metrics.EnableReqLogKey:   "true",
		},
		want: "http://example.com/testpage\n",
	}, {
		name: "invalid template",
		url:  "http://example.com",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "{{}}",
		},
		want: "http://example.com\n",
	}, {
		name: "revision info",
		url:  "http://example.com",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "{{.Revision.Name}}, {{.Revision.Namespace}}, {{.Revision.Service}}, {{.Revision.Configuration}}, {{.Revision.PodName}}, {{.Revision.PodIP}}",
			metrics.EnableReqLogKey:   "true",
		},
		want: "testRevision, testNs, testSvc, testConfig, , \n",
	}, {
		name: "empty template 2",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "",
		},
	}, {
		name: "explicitly enable request logging",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "{{.Request.URL}}\n",
			metrics.EnableReqLogKey:   "true",
		},
		want: "http://example.com/testpage\n",
	}, {
		name: "explicitly disable request logging",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "{{.Request.URL}}\n",
			metrics.EnableReqLogKey:   "false",
		},
	}, {
		name: "explicitly enable request logging with empty template",
		url:  "http://example.com/testpage",
		data: map[string]string{
			metrics.ReqLogTemplateKey: "",
			metrics.EnableReqLogKey:   "true",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf.Reset()
			cm := &corev1.ConfigMap{}
			cm.Data = test.data
			(updateRequestLogFromConfigMap(ltesting.TestLogger(t), handler))(cm)
			resp := httptest.NewRecorder()
			rs := string(uuid.NewUUID())
			req := httptest.NewRequest(http.MethodPost, test.url, bytes.NewBufferString(rs))

			rev := revision(true)
			revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}
			ctx := activatorhandler.WithRevisionAndID(req.Context(), rev, revID)
			handler.ServeHTTP(resp, req.WithContext(ctx))

			if got := buf.String(); got != test.want {
				t.Errorf("got '%v', want '%v'", got, test.want)
			}
		})
	}
}

func TestRequestLogTemplateInputGetter(t *testing.T) {
	tests := []struct {
		name     string
		revision *v1.Revision
		request  *http.Request
		response *pkghttp.RequestLogResponse
		want     pkghttp.RequestLogRevision
	}{{
		name:     "success",
		revision: revision(true),
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
		name:     "labels not found",
		revision: revision(false),
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
			revID := types.NamespacedName{Namespace: test.revision.Namespace, Name: test.revision.Name}
			ctx := activatorhandler.WithRevisionAndID(test.request.Context(), test.revision, revID)
			request := test.request.WithContext(ctx)

			got := requestLogTemplateInputGetter(request, test.response)
			if !cmp.Equal(*got.Revision, test.want) {
				t.Errorf("Got = %v, want: %v, diff:\n%s", got.Revision, test.want, cmp.Diff(got.Revision, test.want))
			}
			if got.Request != request {
				t.Errorf("Got = %v, want: %v", got.Request, request)
			}
			if got.Response != test.response {
				t.Errorf("Got = %v, want: %v", got.Response, test.response)
			}
		})
	}
}

func revision(addLabels bool) *v1.Revision {
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRevisionName,
			Namespace: testNamespaceName,
		},
	}

	if addLabels {
		rev.Labels = map[string]string{
			serving.ConfigurationLabelKey: testConfigName,
			serving.ServiceLabelKey:       testServiceName,
		}
	}

	return rev
}
