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

package feature

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

type States uint8

const (
	// Alpha implies a feature is experimental and may change
	Alpha States = 1 << iota

	// Beta implies a feature is complete but may have unknown bugs or issues
	Beta

	// Stable implies a feature is ready for production
	Stable

	// All flag enables all feature states
	All = Alpha | Beta | Stable
)

func (s States) String() string {
	if s == All {
		return "ALL STATES"
	}

	var b strings.Builder

	for _, entry := range mapping {
		if s&entry.state == 0 {
			continue
		}

		if b.Len() != 0 {
			b.WriteString("|")
		}

		b.WriteString(entry.name)
	}

	return b.String()
}

func (s *States) AddFlags(fs *flag.FlagSet) {
	for _, entry := range mapping {
		flagName := "feature." + strings.ReplaceAll(strings.ToLower(entry.name), " ", "")
		usage := fmt.Sprintf("toggles %q feature assertions", entry.name)
		fs.Var(stateValue{entry.state, s}, flagName, usage)
	}

	fs.Var(stateValue{All, s}, "feature.all", "toggles all features")
}

type stateValue struct {
	mask  States
	value *States
}

func (sv stateValue) Get() interface{} {
	return *sv.value & sv.mask
}

func (sv stateValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}

	if v {
		*sv.value = *sv.value | sv.mask // set
	} else {
		*sv.value = *sv.value &^ sv.mask // clear
	}

	return nil
}

func (sv stateValue) IsBoolFlag() bool {
	return true
}

func (sv stateValue) String() string {
	if sv.value != nil && sv.mask&*sv.value != 0 {
		return "true"
	}

	return "false"
}

var mapping = [...]struct {
	state States
	name  string
}{
	{Alpha, "Alpha"},
	{Beta, "Beta"},
	{Stable, "Stable"},
}
