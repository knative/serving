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
	"container/list"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/test-infra/tools/webhook-apicoverage/coveragecalculator"
	"github.com/knative/test-infra/tools/webhook-apicoverage/resourcetree"
	"github.com/knative/test-infra/tools/webhook-apicoverage/view"
	"go.uber.org/zap"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	// ResourceQueryParam query param name to provide the resource.
	ResourceQueryParam = "resource"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

// APICoverageRecorder type contains resource tree to record API coverage for resources.
type APICoverageRecorder struct {
	Decoder runtime.Decoder
	Logger *zap.SugaredLogger
	resourceForest resourcetree.ResourceForest
}

func (a *APICoverageRecorder) init() {
	a.resourceForest = resourcetree.ResourceForest {
		Version: "v1alpha1",
		ConnectedNodes: make(map[string]*list.List),
		TopLevelTrees: make(map[string]resourcetree.ResourceTree),
	}
	a.addResourceTree("Service", reflect.TypeOf(v1alpha1.Service{}))
	a.addResourceTree("Configuration", reflect.TypeOf(v1alpha1.Configuration{}))
	a.addResourceTree("Route", reflect.TypeOf(v1alpha1.Route{}))
	a.addResourceTree("Revision", reflect.TypeOf(v1alpha1.Revision{}))
}

func (a *APICoverageRecorder) addResourceTree(resource string, t reflect.Type) {
	tree := resourcetree.ResourceTree{
		ResourceName: resource,
		Forest: &a.resourceForest,
	}
	tree.BuildResourceTree(t)
	a.resourceForest.TopLevelTrees[resource] = tree
}

func (a *APICoverageRecorder) recordCoverage(review *v1beta1.AdmissionReview) {
	var (
		v reflect.Value
		resource string
		err error
	)

	switch schemaType := review.Request.Kind.Kind; schemaType {
	case "Service":
		resource = "Service"
		svc := v1alpha1.Service{}
		err = json.Unmarshal(review.Request.Object.Raw, &svc)
		// Even if err is encountered we would just reflect on an empty object.
		v = reflect.ValueOf(svc)
	case "Configuration":
		resource = "Configuration"
		conf := v1alpha1.Configuration{}
		err = json.Unmarshal(review.Request.Object.Raw, &conf)
		v = reflect.ValueOf(conf)
	case "Route":
		resource = "Configuration"
		route := v1alpha1.Route{}
		err = json.Unmarshal(review.Request.Object.Raw, &route)
		v = reflect.ValueOf(route)
	case "Revision":
		resource = "Revision"
		revision := v1alpha1.Revision{}
		err = json.Unmarshal(review.Request.Object.Raw, &revision)
		v = reflect.ValueOf(revision)
	default:
		return
	}

	if err != nil {
		a.Logger.Errorf("Error unmarshalling %s Obj. Request Obj: %s Error: %v", resource, string(review.Request.Object.Raw), err)
		return
	}

	resourceTree := a.resourceForest.TopLevelTrees[resource]
	resourceTree.UpdateCoverage(v)
}

// Since we are not actually validating any API resource object we always return true here.
func (a *APICoverageRecorder) appendReviewResponse(review *v1beta1.AdmissionReview) {
	review.Response = &v1beta1.AdmissionResponse{
		Allowed: true,
		Result: &v1.Status{
			Message: "Welcome aboard!",
		},
	}
}

// RecordResourceCoverage is handler method that records resource coverage.
func (a *APICoverageRecorder) RecordResourceCoverage(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if data, err := ioutil.ReadAll(r.Body); err == nil {
		body = data
	}

	review := &v1beta1.AdmissionReview{}
	_, _, err := a.Decoder.Decode(body, nil, review)
	if err != nil {
		a.Logger.Errorf("Can't decode request: %v", err)
	}
	a.recordCoverage(review)
	a.appendReviewResponse(review)
	responseInBytes, err := json.Marshal(review)

	if _, err := w.Write(responseInBytes); err != nil {
		a.Logger.Errorf("%v", err)
	}
}

// GetResourceCoverage retrieves resource coverage data for the passed in resource via query param.
func (a *APICoverageRecorder) GetResourceCoverage(w http.ResponseWriter, r *http.Request) {
	resource := r.URL.Query().Get(ResourceQueryParam)
	if _, ok := a.resourceForest.TopLevelTrees[resource]; !ok {
		fmt.Fprintf(w, "Resource information not found for resource: %s", resource)
		return
	}

	var ignoreFields coveragecalculator.IgnoredFields
	ignoreFieldsFilePath := "resources/ignoredfields.yaml"
	if err := ignoreFields.ReadFromFile(ignoreFieldsFilePath); err != nil {
		fmt.Fprintf(w, "Error reading file: %s", ignoreFieldsFilePath)
	}

	tree := a.resourceForest.TopLevelTrees[resource]
	typeCoverage := tree.BuildCoverageData(getNodeRules(), getFieldRules(), ignoreFields)

	jsonLikeDisplay := view.GetJSONTypeDisplay(typeCoverage, view.DisplayRules{
		PackageNameRule: PackageDisplayRule,
		FieldRule: FieldDisplayRule,
	})
	fmt.Fprint(w, jsonLikeDisplay)
}