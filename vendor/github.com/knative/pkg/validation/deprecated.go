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

package validation

import (
	"context"
	"github.com/knative/pkg/apis"
	"reflect"
	"strings"
)

const (
	deprecated = "Deprecated"
)

// CheckDeprecated checks whether the provided named deprecated fields
// are set in a context where deprecation is disallowed.
// This is a shallow check.
func CheckDeprecated(ctx context.Context, obj interface{}) *apis.FieldError {
	return CheckDeprecatedUpdate(ctx, obj, nil)
}

// CheckDeprecated checks whether the provided named deprecated fields
// are set in a context where deprecation is disallowed.
// This is a shallow check.
func CheckDeprecatedUpdate(ctx context.Context, obj interface{}, org interface{}) *apis.FieldError {
	if apis.IsDeprecatedAllowed(ctx) {
		return nil
	}

	var errs *apis.FieldError
	newFields := getPrefixedNamedFieldValues(deprecated, obj)

	if nonZero(reflect.ValueOf(org)) {
		orgFields := getPrefixedNamedFieldValues(deprecated, org)

		for name, newValue := range newFields {
			if nonZero(newValue) {
				if differ(orgFields[name], newValue) {
					// Not allowed to update the value.
					errs = errs.Also(apis.ErrDisallowedUpdateDeprecatedFields(name))
				}
			}
		}
	} else {
		for name, value := range newFields {
			if nonZero(value) {
				// Not allowed to set the value.
				errs = errs.Also(apis.ErrDisallowedFields(name))
			}
		}
	}
	return errs
}

func getPrefixedNamedFieldValues(prefix string, obj interface{}) map[string]reflect.Value {
	fields := make(map[string]reflect.Value, 0)

	objValue := reflect.Indirect(reflect.ValueOf(obj))

	// If res is not valid or a struct, don't even try to use it.
	if !objValue.IsValid() || objValue.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < objValue.NumField(); i++ {
		tf := objValue.Type().Field(i)
		if v := objValue.Field(i); v.IsValid() {
			if strings.HasPrefix(tf.Name, prefix) {
				jTag := tf.Tag.Get("json")
				name := strings.Split(jTag, ",")[0]
				if name == "" {
					name = tf.Name
				}
				fields[name] = v
			}
		}
	}
	return fields
}

// nonZero returns true if a is nil or reflect.Zero.
func nonZero(a reflect.Value) bool {
	switch a.Kind() {
	case reflect.Ptr:
		if a.IsNil() {
			return false
		}
		return nonZero(a.Elem())

	case reflect.Map, reflect.Slice, reflect.Array:
		if a.IsNil() {
			return false
		}
		return true

	// This is a nil interface{} type.
	case reflect.Invalid:
		return false

	default:
		if reflect.DeepEqual(a.Interface(), reflect.Zero(a.Type()).Interface()) {
			return false
		}
		return true
	}
}

// differ returns true if a != b
func differ(a, b reflect.Value) bool {
	if a.Kind() != b.Kind() {
		return true
	}

	switch a.Kind() {
	case reflect.Ptr:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() != b.IsNil()
		}
		return differ(a.Elem(), b.Elem())

	default:
		if reflect.DeepEqual(a.Interface(), b.Interface()) {
			return false
		}
		return true
	}
}
