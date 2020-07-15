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

package kflag

import (
	"flag"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
)

type StringSet struct {
	Value sets.String
}

var _ flag.Value = (*StringSet)(nil)

func (i *StringSet) String() string {
	return fmt.Sprintf("%v", i.Value)
}

func (i *StringSet) Set(value string) error {
	if i.Value == nil {
		i.Value = make(sets.String, 1)
	}
	i.Value.Insert(value)
	return nil
}
