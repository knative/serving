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

func TestGitignoresService_List(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gitignore/templates", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `["C", "Go"]`)
	})

	available, _, err := client.Gitignores.List(context.Background())
	if err != nil {
		t.Errorf("Gitignores.List returned error: %v", err)
	}

	want := []string{"C", "Go"}
	if !reflect.DeepEqual(available, want) {
		t.Errorf("Gitignores.List returned %+v, want %+v", available, want)
	}
}

func TestGitignoresService_Get(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gitignore/templates/name", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `{"name":"Name","source":"template source"}`)
	})

	gitignore, _, err := client.Gitignores.Get(context.Background(), "name")
	if err != nil {
		t.Errorf("Gitignores.List returned error: %v", err)
	}

	want := &Gitignore{Name: String("Name"), Source: String("template source")}
	if !reflect.DeepEqual(gitignore, want) {
		t.Errorf("Gitignores.Get returned %+v, want %+v", gitignore, want)
	}
}

func TestGitignoresService_Get_invalidTemplate(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gitignores.Get(context.Background(), "%")
	testURLParseError(t, err)
}
