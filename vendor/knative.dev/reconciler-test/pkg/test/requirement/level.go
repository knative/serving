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

package requirement

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

type Levels uint8

// Descriptions from: https://tools.ietf.org/html/rfc2119

const (
	// Must means that the definition is an absolute requirement of the specification.
	Must Levels = 1 << iota

	// MustNot means that the definition is an absolute prohibition of the specification.
	MustNot

	// Should means that there may exist valid reasons in particular circumstances to
	// ignore a particular item
	Should

	// Should means that there may exist valid reasons in particular circumstances when the
	// particular behavior is acceptable or even useful
	ShouldNot

	// May means that an item is truly optional
	May

	// All flag enables all requirement levels
	All = Must | MustNot | Should | ShouldNot | May
)

func (l Levels) String() string {
	if l == All {
		return "ALL_LEVELS"
	}

	var b strings.Builder

	for _, entry := range mapping {
		if l&entry.level == 0 {
			continue
		}

		if b.Len() != 0 {
			b.WriteString("|")
		}

		b.WriteString(entry.name)
	}

	return b.String()
}

func (l *Levels) AddFlags(fs *flag.FlagSet) {
	for _, entry := range mapping {
		flagName := "requirement." + strings.ReplaceAll(strings.ToLower(entry.name), " ", "")
		usage := fmt.Sprintf("toggles %q requirement assertions", entry.name)
		fs.Var(levelValue{entry.level, l}, flagName, usage)
	}

	fs.Var(levelValue{All, l}, "requirement.all", "toggles all requirement assertions")
}

type levelValue struct {
	mask  Levels
	value *Levels
}

func (lv levelValue) Get() interface{} {
	return *lv.value & lv.mask
}

func (lv levelValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}

	if v {
		*lv.value = *lv.value | lv.mask // set
	} else {
		*lv.value = *lv.value &^ lv.mask // clear
	}

	return nil
}

func (lv levelValue) IsBoolFlag() bool {
	return true
}

func (lv levelValue) String() string {
	if lv.value != nil && lv.mask&*lv.value != 0 {
		return "true"
	}

	return "false"
}

var mapping = [...]struct {
	level Levels
	name  string
}{
	{Must, "MUST"},
	{MustNot, "MUST NOT"},
	{Should, "SHOULD"},
	{ShouldNot, "SHOULD NOT"},
	{May, "MAY"},
}
