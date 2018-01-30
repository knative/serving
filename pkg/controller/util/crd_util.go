/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	"encoding/json"
	"log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// SerializeCustomResource converts from our internal representation to the
// third-party resource representation of a route.
func SerializeCustomResource(r interface{}, ns string) *unstructured.Unstructured {
	log.Printf("Before Conversion: %+v\n", r)
	ret, err := RuntimeObjectToTPRObject(r)
	if err != nil {
		log.Printf("Failed to convert custom resource to runtime Object: %s", err)
		return nil
	}
	log.Printf("After Conversion: %+v\n", ret)
	return ret
}

// TPRObjectToRuntimeObject converts a Kubernetes Third Party Resource
// object into a Runtime object
func TPRObjectToRuntimeObject(o runtime.Object, object interface{}) error {
	m, err := json.Marshal(o)
	if err != nil {
		log.Printf("Failed to marshal %#v : %v", o, err)
		return err
	}

	err = json.Unmarshal(m, &object)
	if err != nil {
		log.Printf("Failed to unmarshal: %v\n", err)
		return err
	}
	return nil
}

// RuntimeObjectToTPRObject converts a Service Controller model object into
// a Kubernetes Third Party Resource object
func RuntimeObjectToTPRObject(object interface{}) (*unstructured.Unstructured, error) {
	m, err := json.Marshal(object)
	if err != nil {
		log.Printf("Failed to marshal %#v : %v", object, err)
		return nil, err
	}
	var ret unstructured.Unstructured
	err = json.Unmarshal(m, &ret)
	if err != nil {
		log.Printf("Failed to unmarshal: %v\n", err)
		return nil, err
	}
	return &ret, nil
}

func CreateCustomResourceClient(config *rest.Config, group *schema.GroupVersion) *dynamic.Client {
	// We need to tweak the configuration so that it points to the right
	// resources under the ThirdPartyResources that Istio uses.
	config.ContentConfig.GroupVersion = group
	config.APIPath = "apis"

	dynClient, err := dynamic.NewClient(config)
	if err != nil {
		log.Printf("Couldn't create dynamic config: %s", err)
		panic("Couldn't create dynamic config")
	}
	return dynClient
}
