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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v3"
	"knative.dev/pkg/configmap"
)

func main() {
	for _, fileName := range os.Args[1:] {
		if err := processFile(fileName); err != nil {
			log.Fatal(err)
		}
	}
}

// processFile reads the ConfigMap manifest from a file and adds or updates the label
// containing the checksum of it's _example data if present.
func processFile(fileName string) error {
	in, err := ioutil.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	out, err := process(in)
	if out == nil || err != nil {
		return err
	}

	// nolint:gosec // This is not security critical so open permissions are fine.
	if err := ioutil.WriteFile(fileName, out, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// process processes a YAML file's bytes and adds or updates the label containing
// the checksum of it's _example data if present.
func process(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}
	content := doc.Content[0]

	example := traverse(content, "data", configmap.ExampleKey)
	if example == nil {
		return nil, nil
	}

	metadata := traverse(content, "metadata")
	if metadata == nil {
		return nil, errors.New("'metadata' not found")
	}

	annotations := traverse(metadata, "annotations")
	if annotations == nil {
		annotations = &yaml.Node{Kind: yaml.MappingNode}
		metadata.Content = append(metadata.Content, strNode("annotations"), annotations)
	}

	checksum := configmap.Checksum(example.Value)
	existingAnnotation := value(annotations, configmap.ExampleChecksumAnnotation)
	if existingAnnotation != nil {
		existingAnnotation.Value = checksum
	} else {
		sumNode := strNode(checksum)
		sumNode.Style = yaml.DoubleQuotedStyle
		annotations.Content = append(annotations.Content,
			strNode(configmap.ExampleChecksumAnnotation), sumNode)
	}

	var buffer bytes.Buffer
	buffer.Grow(len(data))
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, fmt.Errorf("failed to encode YAML: %w", err)
	}
	return buffer.Bytes(), nil
}

// traverse traverses the YAML nodes' children using the given path keys. Returns nil if
// one of the keys can't be found.
func traverse(parent *yaml.Node, path ...string) *yaml.Node {
	if parent == nil || len(path) == 0 {
		return parent
	}
	tail := path[1:]
	child := value(parent, path[0])
	return traverse(child, tail...)
}

// value returns the value of the current node under the given key. Returns nil if not
// found.
func value(parent *yaml.Node, key string) *yaml.Node {
	for i := range parent.Content {
		if parent.Content[i].Value == key {
			// The Content array of a yaml.Node is just an array of more nodes. The keys
			// to each field are therefore a string node right before the relevant field's
			// value, hence we need to return the next value of the array to retrieve
			// the value under a key.
			if len(parent.Content) < i+2 {
				return nil
			}
			return parent.Content[i+1]
		}
	}
	return nil
}

// strNode generate a node that forces the representation to be a string.
func strNode(value string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
}
