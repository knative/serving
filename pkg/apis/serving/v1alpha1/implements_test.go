/*
Copyright 2018 The Knative Authors
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
package v1alpha1

import (
	"testing"

	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
)

func TestTypesImplements(t *testing.T) {
	var emptyGen duckv1alpha1.Generation
	testCases := []struct {
		instance interface{}
		iface    duck.Implementable
	}{
		// Revision
		{instance: &Revision{}, iface: &duckv1alpha1.Conditions{}},
		{instance: &Revision{}, iface: &emptyGen},
		// Configuration
		{instance: &Configuration{}, iface: &duckv1alpha1.Conditions{}},
		{instance: &Configuration{}, iface: &emptyGen},
		// Service
		{instance: &Service{}, iface: &duckv1alpha1.Conditions{}},
		{instance: &Service{}, iface: &duckv1alpha1.LegacyTargetable{}},
		{instance: &Service{}, iface: &duckv1alpha1.Targetable{}},
		{instance: &Service{}, iface: &emptyGen},
		// Route
		{instance: &Route{}, iface: &duckv1alpha1.Conditions{}},
		{instance: &Route{}, iface: &duckv1alpha1.LegacyTargetable{}},
		{instance: &Route{}, iface: &duckv1alpha1.Targetable{}},
		{instance: &Route{}, iface: &emptyGen},
	}
	for _, tc := range testCases {
		if err := duck.VerifyType(tc.instance, tc.iface); err != nil {
			t.Error(err)
		}
	}
}
