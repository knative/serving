/*
Copyright 2021 The Knative Authors

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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
)

const (
	optionalLabelKey = "testing.knative.dev/optional"
	optionalEnvVar   = "KNATIVE_OPTIONAL_RESOURCES"
)

func loadFixtures(path string) ([]*resourceInfo, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	cfg := genericclioptions.NewConfigFlags(true)
	b := resource.NewBuilder(cfg).
		Unstructured().
		Flatten()

	items, err := walkFiles(b, path)
	if err != nil {
		return nil, err
	}

	infos := make([]*resourceInfo, 0, len(items))
	for _, item := range items {
		if include, err := shouldInclude(item); !include {
			continue
		} else if err != nil {
			return nil, err
		}

		r := &resourceInfo{
			Info:   item,
			object: item.Object.(*unstructured.Unstructured),
		}

		infos = append(infos, r)
		if r.isPatch() {
			clone := *item
			err := clone.Get()
			if err != nil {
				return nil, fmt.Errorf("failed fetching server version of %s: %w", r, err)
			}
			r.serverObject = item.Object.(*unstructured.Unstructured)
		}
	}

	return infos, nil
}

func walkFiles(b *resource.Builder, path string) ([]*resource.Info, error) {
	found := false
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		// TODO(dprotaso) follow symlinks
		if err != nil {
			log("skipping traverse of %s: %s", path, err)
			return nil
		}

		switch filepath.Ext(path) {
		case ".yaml", ".yml", "json", ".patch":
		default:
			return nil
		}

		// Trying to do a search replace with unstructured is
		// more complex than replacing the bytes
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %w", err)
		}
		data = interpolateEnvVars(data)
		b.Stream(bytes.NewReader(data), path)
		found = true

		return nil
	})

	if !found {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("traverse failed: %w", err)
	}

	items, err := b.Do().Infos()
	if err != nil {
		return nil, fmt.Errorf("reading resource failed: %w", err)
	}

	return items, err
}

func shouldInclude(r *resource.Info) (bool, error) {
	var set labels.Set = r.Object.(*unstructured.Unstructured).GetLabels()

	// resources that don't have the optional label should not be skipped
	if _, ok := set[optionalLabelKey]; !ok {
		return true, nil
	}

	values := strings.Split(os.Getenv(optionalEnvVar), ",")
	checkLabelValue, err := labels.NewRequirement(optionalLabelKey, selection.In, values)

	if err != nil {
		return false, fmt.Errorf("%s had bad label value: %w", optionalEnvVar, err)
	}

	return checkLabelValue.Matches(set), nil
}

func interpolateEnvVars(input []byte) []byte {
	for _, e := range os.Environ() {
		envVar := strings.SplitN(e, "=", 2)

		key := []byte("${" + envVar[0] + "}")
		val := []byte(envVar[1])

		input = bytes.ReplaceAll(input, key, val)
	}

	return input
}
