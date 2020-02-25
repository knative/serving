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

package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

const (
	// Group specifies the group of the test resource
	Group = "webhook.pkg.knative.dev"

	// Kind specifies the kind of the test resource
	Kind = "Resource"

	// ErrorMarshal when assigned to the Spec.Property of the ErrorResource
	// will cause json marshalling of the resource to fail
	ErrorMarshal = "marshal"

	// ErrorUnmarshal when assigned to the Spec.Property of the ErrorResource
	// will cause json unmarshalling of the resource to fail
	ErrorUnmarshal = "unmarshal"

	// ErrorConvertTo when assigned to the Spec.Property of the ErrorResource
	// will cause ConvertTo to fail
	ErrorConvertTo = "convertTo"

	// ErrorConvertFrom when assigned to the Spec.Property of the ErrorResource
	// will cause ConvertFrom to fail
	ErrorConvertFrom = "convertFrom"
)

type (
	// V1Resource will never has a prefix or suffix on Spec.Property
	// This type is used for testing conversion webhooks
	//
	// +k8s:deepcopy-gen=true
	// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
	V1Resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata,omitempty"`
		Spec              Spec `json:"spec"`
	}

	// V2Resource will always have a 'prefix/' in front of it's property
	// This type is used for testing conversion webhooks
	//
	// +k8s:deepcopy-gen=true
	// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
	V2Resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata,omitempty"`
		Spec              Spec `json:"spec"`
	}

	// V3Resource will always have a '/suffix' in front of it's property
	// This type is used for testing conversion webhooks
	//
	// +k8s:deepcopy-gen=true
	// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
	V3Resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata,omitempty"`
		Spec              SpecWithDefault `json:"spec"`
	}

	// ErrorResource explodes in various settings depending on the property
	// set. Use the Error* constants
	//
	//This type is used for testing conversion webhooks
	//
	// +k8s:deepcopy-gen=true
	// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
	ErrorResource struct {
		// We embed the V1Resource as an easy way to still marshal & unmarshal
		// this type without infinite loops - since we override the methods
		// in order to induce failures
		V1Resource `json:",inline"`
	}

	// Spec holds our fancy string property
	Spec struct {
		Property string `json:"prop"`
	}

	// SpecWithDefault holds two fancy string properties
	SpecWithDefault struct {
		Property    string `json:"prop"`
		NewProperty string `json:"defaulted_prop"`
	}
)

var (
	_ apis.Convertible = (*V1Resource)(nil)
	_ apis.Convertible = (*V2Resource)(nil)
	_ apis.Convertible = (*V3Resource)(nil)
	_ apis.Convertible = (*ErrorResource)(nil)

	_ apis.Defaultable = (*V3Resource)(nil)
)

// NewV1 returns a V1Resource with Spec.Property set
// to prop
func NewV1(prop string) *V1Resource {
	return &V1Resource{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: Group + "/v1",
		},
		Spec: Spec{
			Property: prop,
		},
	}
}

// NewV2 returns a V2Resource with Spec.Property set
// to 'prefix/' + prop
func NewV2(prop string) *V2Resource {
	return &V2Resource{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: Group + "/v2",
		},
		Spec: Spec{
			Property: fmt.Sprintf("prefix/%s", prop),
		},
	}
}

// NewV3 returns a V3Resource with Spec.Property set
// to prop + '/suffix'
func NewV3(prop string) *V3Resource {
	v3 := &V3Resource{
		TypeMeta: metav1.TypeMeta{
			Kind:       Kind,
			APIVersion: Group + "/v3",
		},
		Spec: SpecWithDefault{
			Property: fmt.Sprintf("%s/suffix", prop),
		},
	}
	v3.SetDefaults(context.Background())
	return v3
}

// NewErrorResource returns an ErrorResource with Spec.Property set
// to failure
func NewErrorResource(failure string) *ErrorResource {
	return &ErrorResource{
		V1Resource: V1Resource{
			TypeMeta: metav1.TypeMeta{
				Kind:       Kind,
				APIVersion: Group + "/error",
			},
			Spec: Spec{
				Property: failure,
			},
		},
	}
}

// ConvertTo implements apis.Convertible
func (r *V1Resource) ConvertTo(ctx context.Context, to apis.Convertible) error {
	switch sink := to.(type) {
	case *V2Resource:
		sink.Spec.Property = "prefix/" + r.Spec.Property
	case *V3Resource:
		sink.Spec.Property = r.Spec.Property + "/suffix"
	case *ErrorResource:
		sink.Spec.Property = r.Spec.Property
	case *V1Resource:
		sink.Spec.Property = r.Spec.Property
	default:
		return fmt.Errorf("unsupported type %T", sink)
	}
	return nil
}

// ConvertFrom implements apis.Convertible
func (r *V1Resource) ConvertFrom(ctx context.Context, from apis.Convertible) error {
	switch source := from.(type) {
	case *V2Resource:
		r.Spec.Property = strings.TrimPrefix(source.Spec.Property, "prefix/")
	case *V3Resource:
		r.Spec.Property = strings.TrimSuffix(source.Spec.Property, "/suffix")
	case *ErrorResource:
		r.Spec.Property = source.Spec.Property
	case *V1Resource:
		r.Spec.Property = source.Spec.Property
	default:
		return fmt.Errorf("unsupported type %T", source)
	}
	return nil
}

// SetDefaults implements apis.Defaultable
func (r *V3Resource) SetDefaults(ctx context.Context) {
	if r.Spec.NewProperty == "" {
		r.Spec.NewProperty = "defaulted"
	}
}

// ConvertTo implements apis.Convertible
func (*V2Resource) ConvertTo(ctx context.Context, to apis.Convertible) error {
	panic("unimplemented")
}

// ConvertFrom implements apis.Convertible
func (*V2Resource) ConvertFrom(ctx context.Context, from apis.Convertible) error {
	panic("unimplemented")
}

// ConvertTo implements apis.Convertible
func (*V3Resource) ConvertTo(ctx context.Context, to apis.Convertible) error {
	panic("unimplemented")
}

// ConvertFrom implements apis.Convertible
func (*V3Resource) ConvertFrom(ctx context.Context, from apis.Convertible) error {
	panic("unimplemented")
}

// ConvertTo implements apis.Convertible
func (e *ErrorResource) ConvertTo(ctx context.Context, to apis.Convertible) error {
	if e.Spec.Property == ErrorConvertTo {
		return errors.New("boooom - convert up")
	}

	return e.V1Resource.ConvertTo(ctx, to)
}

// ConvertFrom implements apis.Convertible
func (e *ErrorResource) ConvertFrom(ctx context.Context, from apis.Convertible) error {
	err := e.V1Resource.ConvertFrom(ctx, from)

	if err == nil && e.Spec.Property == ErrorConvertFrom {
		err = errors.New("boooom - convert down")
	}
	return err
}

// UnmarshalJSON implements json.Unmarshaler
func (e *ErrorResource) UnmarshalJSON(data []byte) (err error) {
	err = json.Unmarshal(data, &e.V1Resource)
	if err == nil && e.Spec.Property == ErrorUnmarshal {
		err = errors.New("boooom - unmarshal json")
	}
	return
}

// MarshalJSON implements json.Marshaler
func (e *ErrorResource) MarshalJSON() ([]byte, error) {
	if e.Spec.Property == ErrorMarshal {
		return nil, errors.New("boooom - marshal json")
	}
	return json.Marshal(e.V1Resource)
}
