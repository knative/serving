package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	testing2 "github.com/knative/pkg/logging/testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/google/go-cmp/cmp"

	"github.com/pkg/errors"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	pkghttp "github.com/knative/serving/pkg/http"
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
		requestLogTemplateInputGetter(getRevisionMock(true)))
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
				http.CanonicalHeaderKey(activator.RevisionHeaderName):      {testRevisionName},
				http.CanonicalHeaderKey(activator.RevisionHeaderNamespace): {testNamespaceName},
			}
			handler.ServeHTTP(resp, req)

			got := string(buf.Bytes())
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
		getter: requestLogTemplateInputGetter(getRevisionMock(true)),
		request: &http.Request{Header: map[string][]string{
			http.CanonicalHeaderKey(activator.RevisionHeaderName):      {testRevisionName},
			http.CanonicalHeaderKey(activator.RevisionHeaderNamespace): {testNamespaceName},
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
		getter: requestLogTemplateInputGetter(getRevisionMock(true)),
		request: &http.Request{Header: map[string][]string{
			http.CanonicalHeaderKey(activator.RevisionHeaderName):      {"foo"},
			http.CanonicalHeaderKey(activator.RevisionHeaderNamespace): {"bar"},
		}},
		response: &pkghttp.RequestLogResponse{Code: http.StatusAlreadyReported},
		want: pkghttp.RequestLogRevision{
			Name:      "foo",
			Namespace: "bar",
		},
	}, {
		name:   "labels not found",
		getter: requestLogTemplateInputGetter(getRevisionMock(false)),
		request: &http.Request{Header: map[string][]string{
			http.CanonicalHeaderKey(activator.RevisionHeaderName):      {testRevisionName},
			http.CanonicalHeaderKey(activator.RevisionHeaderNamespace): {testNamespaceName},
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

func getRevisionMock(addLabels bool) func(revID activator.RevisionID) (*v1alpha1.Revision, error) {
	return func(revID activator.RevisionID) (*v1alpha1.Revision, error) {
		rev := &v1alpha1.Revision{}
		if revID.Name != testRevisionName || revID.Namespace != testNamespaceName {
			return nil, errors.New("revision not found")
		}
		rev.Name = revID.Name
		rev.Namespace = revID.Namespace
		if addLabels {
			rev.Labels = map[string]string{
				serving.ConfigurationLabelKey: testConfigName,
				serving.ServiceLabelKey:       testServiceName,
			}
		}
		return rev, nil
	}
}
