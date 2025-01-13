/*
Copyright 2024 The Knative Authors

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
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
)

const resourcesDir = "config/core/300-resources"

func main() {
	files, err := os.ReadDir(resourcesDir)
	if err != nil {
		log.Fatalln("failed to read CRD directory", err)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") || !file.Type().IsRegular() {
			continue
		}

		processFile(file)
	}
}

func processFile(file fs.DirEntry) {
	fmt.Println("Processing", file.Name())

	filename := filepath.Join(resourcesDir, file.Name())

	fbytes, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalln("failed to read CRD", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(fbytes, &root); err != nil {
		log.Fatalln("failed to unmarshal CRD", err)
	}

	buf := bytes.Buffer{}

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	// document node => content[0] => crd map
	applyOverrides(root.Content[0])

	if err = enc.Encode(&root); err != nil {
		log.Fatalln("failed to marshal CRD", err)
	}

	if err = enc.Close(); err != nil {
		log.Fatalln("failed to close yaml encoder", err)
	}

	if err = os.WriteFile(filename, buf.Bytes(), file.Type().Perm()); err != nil {
		log.Fatalln("failed to write CRD", err)
	}
}

func applyOverrides(root *yaml.Node) {
	for _, override := range overrides {
		crdName := stringValue(root, "metadata.name")

		if crdName != override.crdName {
			continue
		}

		versions := arrayValue(root, "spec.versions")

		for _, version := range versions {
			for _, entry := range override.entries {
				schemaNode := mapValue(version, "schema.openAPIV3Schema")
				applyOverrideEntry(schemaNode, entry)
			}
		}
	}
}

func applyOverrideEntry(node *yaml.Node, entry entry) {
	for _, segment := range strings.Split(entry.path, ".") {
		node = children(node)
		node = mapValue(node, segment)

		if node == nil {
			log.Fatalf("node at path %q not found\n", entry.path)
		}
	}

	if node.Kind != yaml.MappingNode {
		log.Fatalf("node at path %q not a mapping node\n", entry.path)
	}

	if entry.description != "" {
		setString(node, "description", entry.description)
	}

	switch dataType(node) {
	case "array":
	case "object":
	default:
		// if we're at a leaf node then other operations are a noop
		return
	}

	dropRequiredFields(node, entry.dropRequired)
	filterAllowedFields(node, entry.allowedFields, entry.featureFlagFields)
	updateFeatureFlags(node, entry.featureFlagFields)

	if entry.dropListType {
		deleteKey(node, "x-kubernetes-list-map-keys")
		deleteKey(node, "x-kubernetes-list-type")
	}
}

func updateFeatureFlags(node *yaml.Node, features []flagField) {
	node = children(node)

	for _, feature := range features {
		propNode := mapValue(node, feature.name)
		updateFeatureFlagProperty(propNode, feature)
	}
}

func updateFeatureFlagProperty(root *yaml.Node, f flagField) {
	desc := "This is accessible behind a feature flag - " + f.flag

	setString(root, "description", desc)

	node := root
	switch dataType(root) {
	case "array":
		node = items(root)
		setString(root, "items.description", desc)
		deleteKeysExcluding(node, "description", "type", "x-kubernetes-map-type")
		deleteKeysExcluding(root, "description", "type", "items")
	case "object":
		if mapValue(node, "properties") == nil {
			// no child elements - so probably a map or dual type (int||str)
			return
		}
		deleteKeysExcluding(node, "description", "type", "x-kubernetes-map-type")
	default:
		return
	}

	node.Content = append(node.Content,
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Style: yaml.FlowStyle,
			Value: "x-kubernetes-preserve-unknown-fields",
		},
		&yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!bool",
			Style: yaml.FlowStyle,
			Value: "true",
		},
	)
}

func filterAllowedFields(node *yaml.Node, allowed sets.Set[string], features []flagField) {
	allowed = allowed.Clone()
	for _, feature := range features {
		allowed.Insert(feature.name)
	}

	if allowed.Len() == 0 {
		return
	}

	propNode := children(node)
	keysToDelete := sets.Set[string]{}

	for i := 0; i < len(propNode.Content); i += 2 {
		keyNode := propNode.Content[i]

		if !allowed.Has(keyNode.Value) {
			keysToDelete.Insert(keyNode.Value)
		}
	}

	for _, key := range keysToDelete.UnsortedList() {
		deleteKey(propNode, key)
	}
}

func dropRequiredFields(node *yaml.Node, fields sets.Set[string]) {
	dataType := dataType(node)

	if dataType == "array" {
		node = items(node)
	}

	required := arrayValue(node, "required")

	if len(required) == 0 {
		deleteKey(node, "required")
		return
	}

	for i := 0; i < len(required); i++ {
		if fields.Has(required[i].Value) {
			required = append(required[:i], required[i+1:]...)
			break
		}
	}

	if len(required) == 0 {
		deleteKey(node, "required")
	} else {
		setArray(node, "required", required)
	}
}
