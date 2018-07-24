/*
Copyright 2017 The Knative Authors

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

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

type cond struct {
	S corev1.ConditionStatus
	R string
	M string
}

type condr map[string]cond

func (c condr) setCondition(typ string, status corev1.ConditionStatus, reason string, message string) {
	c[typ] = cond{
		S: status,
		R: reason,
		M: message,
	}
}

func (c condr) getConditionStatus(typ string) *corev1.ConditionStatus {
	if cd, ok := c[typ]; ok {
		return &cd.S
	}
	return nil
}

func TestConditionManager(t *testing.T) {
	cr := conditionManager{
		ready: "ready",
		subconditions: []string{
			"foo",
			"bar",
		},
	}

	tests := []struct {
		name string
		in   condr
		want condr
		m    func(conditionAccessor, string, string, string)
		typ  string
		r    string
		msg  string
	}{{
		name: "true for one",
		in: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionTrue},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		m:   cr.setTrueCondition,
		typ: "foo",
	}, {
		name: "true cascade",
		in: condr{
			"foo":   {S: corev1.ConditionTrue},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionTrue},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionTrue},
		},
		m:   cr.setTrueCondition,
		typ: "bar",
	}, {
		name: "failure cascade immediately",
		in: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionFalse},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionFalse},
		},
		m:   cr.setFalseCondition,
		typ: "foo",
	}, {
		name: "true does not cascade",
		in: condr{
			"foo":   {S: corev1.ConditionFalse},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionFalse},
		},
		want: condr{
			"foo":   {S: corev1.ConditionFalse},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionFalse},
		},
		m:   cr.setTrueCondition,
		typ: "bar",
	}, {
		name: "unknown resets failure",
		in: condr{
			"foo":   {S: corev1.ConditionFalse},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionFalse},
		},
		want: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		m:   cr.setUnknownCondition,
		typ: "foo",
	}, {
		name: "unknown resets success",
		in: condr{
			"foo":   {S: corev1.ConditionTrue},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionTrue},
		},
		want: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionUnknown},
		},
		m:   cr.setUnknownCondition,
		typ: "foo",
	}, {
		name: "failure propagates reason/message",
		in: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionFalse, R: "asdf", M: "blah"},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionFalse, R: "asdf", M: "blah"},
		},
		m:   cr.setFalseCondition,
		typ: "foo",
		r:   "asdf",
		msg: "blah",
	}, {
		name: "unknown propagates reason/message",
		in: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionUnknown, R: "asdf", M: "blah"},
			"bar":   {S: corev1.ConditionUnknown},
			"ready": {S: corev1.ConditionUnknown, R: "asdf", M: "blah"},
		},
		m:   cr.setUnknownCondition,
		typ: "foo",
		r:   "asdf",
		msg: "blah",
	}, {
		name: "success elides reason/message",
		in: condr{
			"foo":   {S: corev1.ConditionUnknown},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionUnknown},
		},
		want: condr{
			"foo":   {S: corev1.ConditionTrue, R: "asdf", M: "blah"},
			"bar":   {S: corev1.ConditionTrue},
			"ready": {S: corev1.ConditionTrue},
		},
		m:   cr.setTrueCondition,
		typ: "foo",
		r:   "asdf",
		msg: "blah",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			test.m(test.in, test.typ, test.r, test.msg)
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("%s (-got, +want) = %v", test.name, diff)
			}
		})
	}
}
