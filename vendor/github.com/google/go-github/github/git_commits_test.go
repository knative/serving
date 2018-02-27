// Copyright 2013 The go-github AUTHORS. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestGitService_GetCommit(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	acceptHeaders := []string{mediaTypeGitSigningPreview, mediaTypeGraphQLNodeIDPreview}
	mux.HandleFunc("/repos/o/r/git/commits/s", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", strings.Join(acceptHeaders, ", "))
		fmt.Fprint(w, `{"sha":"s","message":"m","author":{"name":"n"}}`)
	})

	commit, _, err := client.Git.GetCommit(context.Background(), "o", "r", "s")
	if err != nil {
		t.Errorf("Git.GetCommit returned error: %v", err)
	}

	want := &Commit{SHA: String("s"), Message: String("m"), Author: &CommitAuthor{Name: String("n")}}
	if !reflect.DeepEqual(commit, want) {
		t.Errorf("Git.GetCommit returned %+v, want %+v", commit, want)
	}
}

func TestGitService_GetCommit_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Git.GetCommit(context.Background(), "%", "%", "%")
	testURLParseError(t, err)
}

func TestGitService_CreateCommit(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &Commit{
		Message: String("m"),
		Tree:    &Tree{SHA: String("t")},
		Parents: []Commit{{SHA: String("p")}},
	}

	mux.HandleFunc("/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		v := new(createCommit)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "POST")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)

		want := &createCommit{
			Message: input.Message,
			Tree:    String("t"),
			Parents: []string{"p"},
		}
		if !reflect.DeepEqual(v, want) {
			t.Errorf("Request body = %+v, want %+v", v, want)
		}
		fmt.Fprint(w, `{"sha":"s"}`)
	})

	commit, _, err := client.Git.CreateCommit(context.Background(), "o", "r", input)
	if err != nil {
		t.Errorf("Git.CreateCommit returned error: %v", err)
	}

	want := &Commit{SHA: String("s")}
	if !reflect.DeepEqual(commit, want) {
		t.Errorf("Git.CreateCommit returned %+v, want %+v", commit, want)
	}
}

func TestGitService_CreateCommit_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Git.CreateCommit(context.Background(), "%", "%", &Commit{})
	testURLParseError(t, err)
}
