/*
Copyright 2022 The Knative Authors

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

package watch

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type CSVWriter struct {
	Directory              string
	ObjectNamePrefixFilter string
	AdditionalColumnTitles func() []string
	AdditionalRowFields    func(*unstructured.Unstructured) []string
}

func (o *CSVWriter) WriteHistory(h GVRHistory) {
	if o.AdditionalColumnTitles == nil {
		o.AdditionalColumnTitles = func() []string {
			return nil
		}
	}
	if o.AdditionalRowFields == nil {
		o.AdditionalRowFields = func(*unstructured.Unstructured) []string {
			return nil
		}
	}

	if err := os.MkdirAll(o.Directory, 0700); err != nil {
		panic(err)
	}

	for gvr, itemHistory := range h {
		var entries [][]string

		for name, events := range itemHistory {
			if !strings.HasPrefix(name, o.ObjectNamePrefixFilter) {
				continue
			}

			readyTimeString, obj, ok := getReadyTime(events)
			if !ok {
				// TODO figure out how to represent this
				continue
			}
			creationTimeString := obj.GetCreationTimestamp().UTC().Format(time.RFC3339)
			creationTime, err := time.Parse(time.RFC3339, creationTimeString)
			if err != nil {
				panic(fmt.Sprint("error parsing creation time ", err))
			}
			readyTime, err := time.Parse(time.RFC3339, readyTimeString)
			if err != nil {
				panic(fmt.Sprint("error parsing ready time ", err))
			}
			row := []string{
				obj.GetName(),
				obj.GetNamespace(),
				creationTimeString,
				readyTimeString,
				strconv.FormatInt(int64(readyTime.Sub(creationTime).Seconds()), 10),
			}

			row = append(row, o.AdditionalRowFields(obj)...)
			entries = append(entries, row)
		}

		// Sort by creationTime
		sort.Slice(entries, func(i, j int) bool {
			return entries[i][2] < entries[j][2]
		})

		filepath := filepath.Join(o.Directory,
			fmt.Sprintf("%s.%s.%s", gvr.Group, gvr.Version, gvr.Resource))

		f, err := os.Create(filepath)
		if err != nil {
			panic(fmt.Sprintf("failed to create file at path %s %s", filepath, err))
		}

		w := csv.NewWriter(f)
		w.Write(append([]string{
			"name",
			"namespace",
			"createdTime",
			"readyTime",
			"timeToReadySeconds",
		}, o.AdditionalColumnTitles()...))
		err = w.WriteAll(entries)
		f.Close()

		if err != nil {
			panic(fmt.Sprintf("failed to write csv entries %s", err))
		}
	}
}

func getReadyTime(events []Event) (string, *unstructured.Unstructured, bool) {
	for _, event := range events {
		conditionType := "Ready"
		if strings.EqualFold(event.Object.GetKind(), "deployment") {
			conditionType = "Active"
		}
		readyTime, ok := isConditionTrue(event.Object, conditionType)
		if ok {
			return readyTime, event.Object, ok
		}
	}
	return "", nil, false
}

func isConditionTrue(u *unstructured.Unstructured, conditionType string) (string, bool) {
	cs, ok, err := unstructured.NestedFieldNoCopy(u.Object, "status", "conditions")
	if !ok || err != nil {
		return "", false
	}
	conditions := cs.([]interface{})

	for _, c := range conditions {
		condition := c.(map[string]interface{})

		status, ok, err := unstructured.NestedString(condition, "status")
		if !ok || err != nil {
			panic(fmt.Sprintf("Unable to read status ok (%t) err (%v)", ok, err))
		}
		tipe, ok, err := unstructured.NestedString(condition, "type")
		if !ok || err != nil {
			panic(fmt.Sprintf("Unable to read type ok (%t) err (%v)", ok, err))
		}
		lastTransitionTime, ok, err := unstructured.NestedString(condition, "lastTransitionTime")
		if !ok || err != nil {
			panic(fmt.Sprintf("Unable to read lastTransitionTime ok (%t) err (%v)", ok, err))
		}
		if tipe == conditionType && status == "True" {
			return lastTransitionTime, true
		}
	}
	return "", false
}
