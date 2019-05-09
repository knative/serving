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

package resourcetree

import (
	"reflect"
)

// node.go contains types and interfaces pertaining to nodes inside resource tree.

// NodeInterface defines methods that can be performed on each node in the resource tree.
type NodeInterface interface {
	GetData() NodeData
	initialize(field string, parent NodeInterface, t reflect.Type, rt *ResourceTree)
	buildChildNodes(t reflect.Type)
	updateCoverage(v reflect.Value)
	buildCoverageData(coverageDataHelper coverageDataHelper)
	getValues() map[string]bool
}

// NodeData is the data stored in each node of the resource tree.
type NodeData struct {
	Field     string                   // Represents the Name of the field e.g. field name inside the struct.
	Tree      *ResourceTree            // Reference back to the resource tree. Required for cross-tree traversal(connected nodes traversal)
	FieldType reflect.Type             // Required as type information is not available during tree traversal.
	NodePath  string                   // Path in the resource tree reaching this node.
	Parent    NodeInterface            // Link back to parent.
	Children  map[string]NodeInterface // Child nodes are keyed using field names(nodeData.field).
	LeafNode  bool                     // Storing this as an additional field because type-analysis determines the value, which gets used later in value-evaluation
	Covered   bool
}

func (nd *NodeData) initialize(field string, parent NodeInterface, t reflect.Type, rt *ResourceTree) {
	nd.Field = field
	nd.Tree = rt
	nd.Parent = parent
	nd.FieldType = t
	nd.Children = make(map[string]NodeInterface)

	if parent != nil {
		nd.NodePath = parent.GetData().NodePath + "." + field
	} else {
		nd.NodePath = field
	}
}
