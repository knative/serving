/*
Copyright 2020 The Knative Authors

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

package roundtrip

import (
	"context"
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/api/apitesting/roundtrip"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metafuzzer "k8s.io/apimachinery/pkg/apis/meta/fuzzer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/apis"
)

type convertibleObject interface {
	runtime.Object
	apis.Convertible
}

// globalNonRoundTrippableTypes are kinds that are effectively reserved
// across all GroupVersions. They don't roundtrip
//
// This list comes from k8s.io/apimachinery. We can drop this constant when
// the PR (https://github.com/kubernetes/kubernetes/pull/86959) merges and
// we bump to a version that has the change
var globalNonRoundTrippableTypes = sets.NewString(
	"ExportOptions",
	"GetOptions",
	// WatchEvent does not include kind and version and can only be deserialized
	// implicitly (if the caller expects the specific object). The watch call defines
	// the schema by content type, rather than via kind/version included in each
	// object.
	"WatchEvent",
	// ListOptions is now part of the meta group
	"ListOptions",
	// Delete options is only read in metav1
	"DeleteOptions",
)

var (
	metaV1Types    map[reflect.Type]struct{}
	metaV1ListType = reflect.TypeOf((*metav1.ListMetaAccessor)(nil)).Elem()
)

func init() {
	gv := schema.GroupVersion{Group: "roundtrip.group", Version: "v1"}

	scheme := runtime.NewScheme()

	metav1.AddToGroupVersion(scheme, gv)
	metaV1Types = make(map[reflect.Type]struct{})

	// Build up a list of types to ignore
	for _, t := range scheme.KnownTypes(gv) {
		metaV1Types[t] = struct{}{}
	}
}

// ExternalTypesViaJSON applies the round-trip test to all external round-trippable Kinds
// in the scheme. This is effectively testing the scenario:
//
//    external -> json -> external
//
func ExternalTypesViaJSON(t *testing.T, scheme *runtime.Scheme, fuzzerFuncs fuzzer.FuzzerFuncs) {
	codecFactory := serializer.NewCodecFactory(scheme)

	f := fuzzer.FuzzerFor(
		fuzzer.MergeFuzzerFuncs(metafuzzer.Funcs, fuzzerFuncs),
		rand.NewSource(rand.Int63()),
		codecFactory,
	)

	f.SkipFieldsWithPattern(regexp.MustCompile("DeprecatedGeneration"))

	kinds := scheme.AllKnownTypes()
	for gvk := range kinds {
		if gvk.Version == runtime.APIVersionInternal || globalNonRoundTrippableTypes.Has(gvk.Kind) {
			continue
		}
		t.Run(gvk.Group+"."+gvk.Version+"."+gvk.Kind, func(t *testing.T) {
			roundtrip.RoundTripSpecificKindWithoutProtobuf(t, gvk, scheme, codecFactory, f, nil)
		})
	}
}

// ExternalTypesViaHub applies the round-trip test to all external round-trippable Kinds
// in the scheme. This is effectively testing the scenario:
//
//    external version -> hub version -> external version
//
func ExternalTypesViaHub(t *testing.T, scheme, hubs *runtime.Scheme, fuzzerFuncs fuzzer.FuzzerFuncs) {
	f := fuzzer.FuzzerFor(
		fuzzer.MergeFuzzerFuncs(metafuzzer.Funcs, fuzzerFuncs),
		rand.NewSource(rand.Int63()),
		// This seems to be used for protobuf not json
		serializer.NewCodecFactory(scheme),
	)

	f.SkipFieldsWithPattern(regexp.MustCompile("DeprecatedGeneration"))

	for gvk, objType := range scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal ||
			gvk.Group == "" || // K8s group
			globalNonRoundTrippableTypes.Has(gvk.Kind) {
			continue
		}

		if _, ok := metaV1Types[objType]; ok {
			continue
		}

		if reflect.PtrTo(objType).AssignableTo(metaV1ListType) {
			continue
		}

		if hubs.Recognizes(gvk) {
			continue
		}

		t.Run(gvk.Group+"."+gvk.Version+"."+gvk.Kind, func(t *testing.T) {
			for i := 0; i < *roundtrip.FuzzIters; i++ {
				roundTripViaHub(t, gvk, scheme, hubs, f)

				if t.Failed() {
					break
				}
			}
		})
	}
}

func roundTripViaHub(t *testing.T, gvk schema.GroupVersionKind, scheme, hubs *runtime.Scheme, f *fuzz.Fuzzer) {
	ctx := context.Background()

	hub, hubGVK := hubInstanceForGK(t, hubs, gvk.GroupKind())
	obj := objForGVK(t, gvk, scheme)

	fuzzObject(t, f, gvk, obj)

	original := obj
	obj = obj.DeepCopyObject().(convertibleObject)

	if !apiequality.Semantic.DeepEqual(original, obj) {
		t.Errorf("DeepCopy altered the object, diff: %v", diff(original, obj))
		return
	}

	if err := hub.ConvertFrom(ctx, obj); err != nil {
		t.Errorf("Conversion to hub (%s) failed: %s", hubGVK, err)
	}

	if !apiequality.Semantic.DeepEqual(original, obj) {
		t.Errorf("Conversion to hub (%s) alterted the object, diff: %v", hubGVK, diff(original, obj))
		return
	}

	newObj := objForGVK(t, gvk, scheme)
	if err := hub.ConvertTo(ctx, newObj); err != nil {
		t.Errorf("Conversion from hub (%s) failed: %s", hubGVK, err)
		t.Errorf("object: %#v", obj)
		return
	}

	if !apiequality.Semantic.DeepEqual(obj, newObj) {
		t.Errorf("round trip through hub (%s) produced a diff: %s", hubGVK, diff(original, newObj))
		return
	}
}

func diff(obj1, obj2 interface{}) string {
	// knative.dev/pkg/apis.URL is an alias to net.URL which embeds a
	// url.Userinfo that has an unexported field
	return cmp.Diff(obj1, obj2, cmpopts.IgnoreUnexported(url.Userinfo{}))
}

func objForGVK(t *testing.T,
	gvk schema.GroupVersionKind,
	scheme *runtime.Scheme,
) convertibleObject {

	t.Helper()

	obj, err := scheme.New(gvk)
	if err != nil {
		t.Fatalf("unable to create object instance for type %s", err)
	}

	objType, err := apimeta.TypeAccessor(obj)
	if err != nil {
		t.Fatalf("%q is not a TypeMeta and cannot be tested: %v", gvk, err)
	}
	objType.SetKind(gvk.Kind)
	objType.SetAPIVersion(gvk.GroupVersion().String())
	return obj.(convertibleObject)
}

func fuzzObject(t *testing.T, fuzzer *fuzz.Fuzzer, gvk schema.GroupVersionKind, obj interface{}) {
	fuzzer.Fuzz(obj)

	objType, err := apimeta.TypeAccessor(obj)
	if err != nil {
		t.Fatalf("%q is not a TypeMeta and cannot be tested: %v", gvk, err)
	}
	objType.SetKind(gvk.Kind)
	objType.SetAPIVersion(gvk.GroupVersion().String())
}

func hubInstanceForGK(t *testing.T,
	hubs *runtime.Scheme,
	gk schema.GroupKind,
) (apis.Convertible, schema.GroupVersionKind) {

	t.Helper()

	for hubGVK := range hubs.AllKnownTypes() {
		if hubGVK.GroupKind() == gk {
			obj, err := hubs.New(hubGVK)
			if err != nil {
				t.Fatalf("error creating objects %s", err)
			}

			return obj.(apis.Convertible), hubGVK
		}
	}

	t.Fatalf("hub type not found")
	return nil, schema.GroupVersionKind{}
}
