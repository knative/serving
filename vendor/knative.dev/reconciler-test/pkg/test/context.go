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

package test

import (
	"flag"
	"reflect"
	"testing"

	"knative.dev/pkg/test/helpers"
	"knative.dev/reconciler-test/pkg/test/feature"
	"knative.dev/reconciler-test/pkg/test/requirement"
)

// T extends testing.T with additional behaviour to annotate
// assertions with various requirement levels and feature states.
// Downstream implemenations should create their own struct that
// embeds type T and include their own additional properties.
//
//  type MyT struct {
//    test.T
//
//    /* additional properties */
//    environment.Cluster
//  }
//
// Per test setup can be accomplished by adding a Setup(*testing.T) method
//
//  func (m *MyT) Setup(t *testing.T) {
//    / * init some clients */
//  }
//
// Authors should avoid globals and duplicate setup code by
// leveraging this contextual test struct. This allows tests
// to be authored in the following way:
//
//   func TestSomeFeature(t *MyT) {
//       if err := t.Cluster.KubeClient.Create(...); err != nil {
//         t.Fatal(err)
//       }
//
//       t.Must("have some side-effect", func(t *MyT) {...}
//       t.May("do something optional", func(t *MyT) {...}
//       t.Alpha("subfeature", func(t *MyT) {...}
//   }
//
// To run your tests with T you can setup it as follows:
//
//   func TestConformance(t *testing.T) {
//     myT := MyT{}
//     test.Init(MyT{}, t)
//
//     myT.Stable("some feature", TestFeature)
//   }
//
//  func TestFeature(t *MyT) {
//    ...
//  }
//
// If users want to customize the test invocation via the
// `go test` command line you will need to add flags to
// the global FlagSet - flag.CommandLine
//
// In a test file create a package instance variable
// and in an `init` function add your flags to flag.CommandLine
//
//   var myT = MyT{}
//
//   func init() {
//     myT.AddFlags(flag.CommandLine)
//   }
//
type T struct {
	*testing.T

	RequirementLevels requirement.Levels
	FeatureStates     feature.States

	// Implemention note: we need a reference to the derived struct
	// so we can make shallow copies for subtests
	self interface{}
}

// Init initializes types that embed T with an testing.T instance.
// If c has an optional Setup(*testing.T) method it will be invoked.
//
// This function will panic if c does not embed T
func Init(c interface{}, t *testing.T) {
	type (
		internal interface {
			set(c interface{}, t *testing.T)
		}

		setup interface {
			Setup(t *testing.T)
		}
	)

	if i, ok := c.(internal); ok {
		i.set(c, t)
	} else {
		panic("Init should be invoked with a struct that embeds test.T")
	}

	if s, ok := c.(setup); ok {
		s.Setup(t)
	}
}

func (t *T) set(c interface{}, gotest *testing.T) {
	t.self = c
	t.T = gotest
}

// AddFlags adds requirement and feature state flags to the FlagSet.
// The flagset will modify this context instance
//
// Calling AddFlags will also default the requirement level and
// feature states to test everything
func (t *T) AddFlags(fs *flag.FlagSet) {
	if t.RequirementLevels == 0 {
		t.RequirementLevels = requirement.All
	}
	if t.FeatureStates == 0 {
		t.FeatureStates = feature.All
	}

	t.RequirementLevels.AddFlags(fs)
	t.FeatureStates.AddFlags(fs)
}

// Must invokes f as a subtest if the context has the requirement level MUST
func (t *T) Must(name string, f interface{}) bool {
	t.Helper()
	return t.invokeLevel(requirement.Must, name, f)
}

// MustNot invokes f as a subtest only if the context has the requirement level MUST NOT
func (t *T) MustNot(name string, f interface{}) bool {
	t.Helper()
	return t.invokeLevel(requirement.MustNot, name, f)
}

// Should invokes f as a subtest only if the context has the requirement level SHOULD
func (t *T) Should(name string, f interface{}) bool {
	t.Helper()
	return t.invokeLevel(requirement.Should, name, f)
}

// ShouldNot invokes f as a subtest only if the context has the requirement level SHOULD NOT
func (t *T) ShouldNot(name string, f interface{}) bool {
	t.Helper()
	return t.invokeLevel(requirement.ShouldNot, name, f)
}

// May invokes f as a subtest only if the context has the requirement level MAY
func (t *T) May(name string, f interface{}) bool {
	t.Helper()
	return t.invokeLevel(requirement.May, name, f)
}

// Alpha invokes f as a subtest only if the context has the 'Alpha' feature state enabled
func (t *T) Alpha(name string, f interface{}) bool {
	t.Helper()
	return t.invokeFeature(feature.Alpha, name, f)
}

// Beta invokes f as a subtest only if the context has the 'Beta' feature state enabled
func (t *T) Beta(name string, f interface{}) bool {
	t.Helper()
	return t.invokeFeature(feature.Beta, name, f)
}

// Stable invokes f as a subtest only if the context has the 'Stable' feature state enabled
func (t *T) Stable(name string, f interface{}) bool {
	t.Helper()
	return t.invokeFeature(feature.Stable, name, f)
}

// ObjectNameForTest returns a unique resource name based on the test name
func (t *T) ObjectNameForTest() string {
	return helpers.ObjectNameForTest(t.T)
}

// Run invokes f as a subtest
func (t *T) Run(name string, f interface{}) bool {
	t.Helper()
	t.validateCallback(f)

	return t.T.Run(name, func(test *testing.T) {
		t.invoke(f, test)
	})
}

func (t *T) invokeFeature(state feature.States, name string, f interface{}) bool {
	t.Helper()
	t.validateCallback(f)

	return t.T.Run(name, func(test *testing.T) {
		if t.FeatureStates&state == 0 {
			test.Skipf("%s features not enabled for testing", state)
		}
		t.invoke(f, test)
	})
}

func (t *T) invokeLevel(levels requirement.Levels, name string, f interface{}) bool {
	t.Helper()
	t.validateCallback(f)

	return t.T.Run(name, func(test *testing.T) {
		if t.RequirementLevels&levels == 0 {
			test.Skipf("%s requirement not enabled for testing", levels)
		}

		t.invoke(f, test)
	})
}

func (t *T) invoke(f interface{}, test *testing.T) {
	newCtx := shallowCopy(t.self)
	Init(newCtx, test)

	in := []reflect.Value{reflect.ValueOf(newCtx)}
	reflect.ValueOf(f).Call(in)
}

func (t *T) validateCallback(f interface{}) {
	t.Helper()

	if f == nil {
		t.Fatal("callback should not be nil")
	}

	fType := reflect.TypeOf(f)
	if fType.Kind() != reflect.Func {
		t.Fatal("callback should be a function")
	}

	contextType := reflect.TypeOf(t.self)

	if fType.NumIn() != 1 || fType.In(0) != contextType {
		t.Fatalf("callback should take a single argument of %v", contextType)
	}
}

func shallowCopy(x interface{}) interface{} {
	// non-pointer type
	derivedType := reflect.TypeOf(x).Elem()
	derivedValue := reflect.ValueOf(x)

	// as if calling new(derivedType)
	y := reflect.New(derivedType)

	// *y = *x
	// both are pointer to struct types so it's a shallow value copy
	derefSource := reflect.Indirect(derivedValue)
	derefTarget := reflect.Indirect(y)
	derefTarget.Set(derefSource)

	return y.Interface()
}
