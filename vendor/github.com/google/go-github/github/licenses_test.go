// Copyright 2013 The go-github AUTHORS. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package github

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestLicensesService_List(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/licenses", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeLicensesPreview)
		fmt.Fprint(w, `[{"key":"mit","name":"MIT","spdx_id":"MIT","url":"https://api.github.com/licenses/mit","featured":true}]`)
	})

	licenses, _, err := client.Licenses.List(context.Background())
	if err != nil {
		t.Errorf("Licenses.List returned error: %v", err)
	}

	want := []*License{{
		Key:      String("mit"),
		Name:     String("MIT"),
		SPDXID:   String("MIT"),
		URL:      String("https://api.github.com/licenses/mit"),
		Featured: Bool(true),
	}}
	if !reflect.DeepEqual(licenses, want) {
		t.Errorf("Licenses.List returned %+v, want %+v", licenses, want)
	}
}

func TestLicensesService_Get(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/licenses/mit", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeLicensesPreview)
		fmt.Fprint(w, `{"key":"mit","name":"MIT"}`)
	})

	license, _, err := client.Licenses.Get(context.Background(), "mit")
	if err != nil {
		t.Errorf("Licenses.Get returned error: %v", err)
	}

	want := &License{Key: String("mit"), Name: String("MIT")}
	if !reflect.DeepEqual(license, want) {
		t.Errorf("Licenses.Get returned %+v, want %+v", license, want)
	}
}

func TestLicensesService_Get_invalidTemplate(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Licenses.Get(context.Background(), "%")
	testURLParseError(t, err)
}
