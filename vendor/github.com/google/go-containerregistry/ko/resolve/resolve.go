// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resolve

import (
	"bytes"
	"io"
	"sync"

	"gopkg.in/yaml.v2"

	"github.com/google/go-containerregistry/ko/build"
	"github.com/google/go-containerregistry/ko/publish"
)

// ImageReferences resolves supported references to images within the input yaml
// to published image digests.
func ImageReferences(input []byte, builder build.Interface, publisher publish.Interface) ([]byte, error) {
	// The loop is to support multi-document yaml files.
	// This is handled by using a yaml.Decoder and reading objects until io.EOF, see:
	// https://github.com/go-yaml/yaml/blob/v2.2.1/yaml.go#L124
	decoder := yaml.NewDecoder(bytes.NewBuffer(input))
	buf := bytes.NewBuffer(nil)
	encoder := yaml.NewEncoder(buf)
	for {
		var obj interface{}
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				return buf.Bytes(), nil
			}
			return nil, err
		}

		// Recursively walk input, building and publishing supported references.
		// TODO(mattmoor): It may be worth considering gathering the supported references in
		// a first pass, performing the builds and pushes, and then performing the
		// substitutions in a second pass.  This would enable us to eliminate the goroutine
		// logic embedded in the recursive walk, naturally eliminate redundant build / publish
		// for the same reference, and potentially enable us to create a single larger build
		// (not clear if/how much this is possible in go build).
		obj2, err := replaceRecursive(obj, func(ref string) (string, error) {
			if !builder.IsSupportedReference(ref) {
				return ref, nil
			}
			img, err := builder.Build(ref)
			if err != nil {
				return "", err
			}
			digest, err := publisher.Publish(img, ref)
			if err != nil {
				return "", err
			}
			return digest.String(), nil
		})
		if err != nil {
			return nil, err
		}

		if err := encoder.Encode(obj2); err != nil {
			return nil, err
		}
	}
}

type replaceString func(string) (string, error)

// replaceRecursive walks the provided untyped object recursively by switching
// on the type of the object at each level. It supports walking through the
// keys and values of maps, and the elements of an array. When a leaf of type
// string is encountered, this will call the provided replaceString function on
// it. This function does not support walking through struct types, but also
// should not need to as the input is expected to be the result of parsing yaml
// or json into an interface{}, which should only produce primitives, maps and
// arrays. This function will return a copy of the object rebuilt by the walk
// with the replacements made.
func replaceRecursive(obj interface{}, rs replaceString) (interface{}, error) {
	switch typed := obj.(type) {
	case map[interface{}]interface{}:
		var sm sync.Map
		var wg sync.WaitGroup
		var mapError error
		// Launch go routines for all of the key/value pairs.
		// We could perhaps further parallelize by doing keys and values
		// in parallel, but the likelihood of that materially improving
		// things is vanishingly small since most keys are fixed in K8s
		// anyways.
		for k, v := range typed {
			wg.Add(1)
			go func(k, v interface{}) {
				defer wg.Done()

				k2, err := replaceRecursive(k, rs)
				if err != nil {
					mapError = err
					return
				}
				v2, err := replaceRecursive(v, rs)
				if err != nil {
					mapError = err
					return
				}
				sm.Store(k2, v2)
			}(k, v)
		}
		// Wait for all of the go routines to complete.
		wg.Wait()
		if mapError != nil {
			return nil, mapError
		}
		// Load what we put into our sync.Map into a normal map.
		m2 := make(map[interface{}]interface{}, len(typed))
		sm.Range(func(k, v interface{}) bool {
			m2[k] = v
			return true // keep going
		})
		return m2, nil

	case []interface{}:
		a2 := make([]interface{}, len(typed))
		var wg sync.WaitGroup
		var arrayError error
		// Launch go routines for each entry in the array.
		for idx, v := range typed {
			wg.Add(1)
			go func(idx int, v interface{}) {
				defer wg.Done()

				v2, err := replaceRecursive(v, rs)
				if err != nil {
					arrayError = err
					return
				}
				a2[idx] = v2
			}(idx, v)
		}
		// Wait for all of the go routines to complete.
		wg.Wait()
		if arrayError != nil {
			return nil, arrayError
		}
		return a2, nil

	case string:
		// call our replaceString on this string leaf.
		return rs(typed)

	default:
		// leave other leaves alone.
		return typed, nil
	}
}
