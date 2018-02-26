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
	"testing"
)

func TestRepositoriesService_ListComments(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/repos/o/r/comments", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeReactionsPreview)
		testFormValues(t, r, values{"page": "2"})
		fmt.Fprint(w, `[{"id":1}, {"id":2}]`)
	})

	opt := &ListOptions{Page: 2}
	comments, _, err := client.Repositories.ListComments(context.Background(), "o", "r", opt)
	if err != nil {
		t.Errorf("Repositories.ListComments returned error: %v", err)
	}

	want := []*RepositoryComment{{ID: Int64(1)}, {ID: Int64(2)}}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("Repositories.ListComments returned %+v, want %+v", comments, want)
	}
}

func TestRepositoriesService_ListComments_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Repositories.ListComments(context.Background(), "%", "%", nil)
	testURLParseError(t, err)
}

func TestRepositoriesService_ListCommitComments(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/repos/o/r/commits/s/comments", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeReactionsPreview)
		testFormValues(t, r, values{"page": "2"})
		fmt.Fprint(w, `[{"id":1}, {"id":2}]`)
	})

	opt := &ListOptions{Page: 2}
	comments, _, err := client.Repositories.ListCommitComments(context.Background(), "o", "r", "s", opt)
	if err != nil {
		t.Errorf("Repositories.ListCommitComments returned error: %v", err)
	}

	want := []*RepositoryComment{{ID: Int64(1)}, {ID: Int64(2)}}
	if !reflect.DeepEqual(comments, want) {
		t.Errorf("Repositories.ListCommitComments returned %+v, want %+v", comments, want)
	}
}

func TestRepositoriesService_ListCommitComments_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Repositories.ListCommitComments(context.Background(), "%", "%", "%", nil)
	testURLParseError(t, err)
}

func TestRepositoriesService_CreateComment(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &RepositoryComment{Body: String("b")}

	mux.HandleFunc("/repos/o/r/commits/s/comments", func(w http.ResponseWriter, r *http.Request) {
		v := new(RepositoryComment)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "POST")
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w, `{"id":1}`)
	})

	comment, _, err := client.Repositories.CreateComment(context.Background(), "o", "r", "s", input)
	if err != nil {
		t.Errorf("Repositories.CreateComment returned error: %v", err)
	}

	want := &RepositoryComment{ID: Int64(1)}
	if !reflect.DeepEqual(comment, want) {
		t.Errorf("Repositories.CreateComment returned %+v, want %+v", comment, want)
	}
}

func TestRepositoriesService_CreateComment_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Repositories.CreateComment(context.Background(), "%", "%", "%", nil)
	testURLParseError(t, err)
}

func TestRepositoriesService_GetComment(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/repos/o/r/comments/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeReactionsPreview)
		fmt.Fprint(w, `{"id":1}`)
	})

	comment, _, err := client.Repositories.GetComment(context.Background(), "o", "r", 1)
	if err != nil {
		t.Errorf("Repositories.GetComment returned error: %v", err)
	}

	want := &RepositoryComment{ID: Int64(1)}
	if !reflect.DeepEqual(comment, want) {
		t.Errorf("Repositories.GetComment returned %+v, want %+v", comment, want)
	}
}

func TestRepositoriesService_GetComment_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Repositories.GetComment(context.Background(), "%", "%", 1)
	testURLParseError(t, err)
}

func TestRepositoriesService_UpdateComment(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &RepositoryComment{Body: String("b")}

	mux.HandleFunc("/repos/o/r/comments/1", func(w http.ResponseWriter, r *http.Request) {
		v := new(RepositoryComment)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "PATCH")
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w, `{"id":1}`)
	})

	comment, _, err := client.Repositories.UpdateComment(context.Background(), "o", "r", 1, input)
	if err != nil {
		t.Errorf("Repositories.UpdateComment returned error: %v", err)
	}

	want := &RepositoryComment{ID: Int64(1)}
	if !reflect.DeepEqual(comment, want) {
		t.Errorf("Repositories.UpdateComment returned %+v, want %+v", comment, want)
	}
}

func TestRepositoriesService_UpdateComment_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Repositories.UpdateComment(context.Background(), "%", "%", 1, nil)
	testURLParseError(t, err)
}

func TestRepositoriesService_DeleteComment(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/repos/o/r/comments/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "DELETE")
	})

	_, err := client.Repositories.DeleteComment(context.Background(), "o", "r", 1)
	if err != nil {
		t.Errorf("Repositories.DeleteComment returned error: %v", err)
	}
}

func TestRepositoriesService_DeleteComment_invalidOwner(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Repositories.DeleteComment(context.Background(), "%", "%", 1)
	testURLParseError(t, err)
}
