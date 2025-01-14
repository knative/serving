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
	"log"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
)

func mapValue(node *yaml.Node, path string) *yaml.Node {
	segments := strings.Split(path, ".")

outer:
	for _, segment := range segments {
		if node.Kind != yaml.MappingNode {
			log.Panicf("node at segment %q not a mapping node\n", segment)
		}

		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			if keyNode.Value == segment {
				node = valueNode
				continue outer
			}
		}

		return nil
	}

	return node
}

func stringValue(node *yaml.Node, path string) string {
	if path != "" {
		node = mapValue(node, path)
	}

	if node == nil {
		log.Panicf("node at path %q not found\n", path)
	}

	if node.Kind != yaml.ScalarNode {
		log.Panicf("node at path %q not a scalar node\n", path)
	}

	if node.ShortTag() != "!!str" {
		log.Panicf("node at path %q not a string node\n", path)
	}

	return node.Value
}

func arrayValue(node *yaml.Node, path string) []*yaml.Node {
	node = mapValue(node, path)

	if node == nil {
		return nil
	}

	if node.Kind != yaml.SequenceNode {
		log.Panicf("node at path %q not a sequence node\n", path)
	}

	return node.Content
}

func setArray(node *yaml.Node, path string, values []*yaml.Node) {
	node = mapValue(node, path)

	if node.Kind != yaml.SequenceNode {
		log.Panicf("node at path %q not a sequence node\n", path)
	}

	node.Content = values
}

func deleteKey(node *yaml.Node, key string) {
	if node.Kind != yaml.MappingNode {
		log.Panicf("node is not mapping node")
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		if keyNode.Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func children(node *yaml.Node) *yaml.Node {
	dataType := dataType(node)

	switch dataType {
	case "object":
		return properties(node)
	case "array":
		return properties(items(node))
	default:
		log.Panicf("node has no children")
		return nil
	}
}

func properties(node *yaml.Node) *yaml.Node {
	return mapValue(node, "properties")
}

func items(node *yaml.Node) *yaml.Node {
	return mapValue(node, "items")
}

func dataType(node *yaml.Node) string {
	return stringValue(node, "type")
}

func setString(node *yaml.Node, path, value string) {
	if path != "" {
		node = mapValue(node, path)
	}

	node.Value = value
}

func deleteKeysExcluding(node *yaml.Node, keys ...string) {
	keySet := sets.New(keys...)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]

		if !keySet.Has(keyNode.Value) {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			i -= 2 // reset index
		}
	}
}
