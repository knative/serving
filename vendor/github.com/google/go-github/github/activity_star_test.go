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
	"time"
)

func TestActivityService_ListStargazers(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/repos/o/r/stargazers", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeStarringPreview)
		testFormValues(t, r, values{
			"page": "2",
		})

		fmt.Fprint(w, `[{"starred_at":"2002-02-10T15:30:00Z","user":{"id":1}}]`)
	})

	stargazers, _, err := client.Activity.ListStargazers(context.Background(), "o", "r", &ListOptions{Page: 2})
	if err != nil {
		t.Errorf("Activity.ListStargazers returned error: %v", err)
	}

	want := []*Stargazer{{StarredAt: &Timestamp{time.Date(2002, time.February, 10, 15, 30, 0, 0, time.UTC)}, User: &User{ID: Int64(1)}}}
	if !reflect.DeepEqual(stargazers, want) {
		t.Errorf("Activity.ListStargazers returned %+v, want %+v", stargazers, want)
	}
}

func TestActivityService_ListStarred_authenticatedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/starred", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeStarringPreview)
		fmt.Fprint(w, `[{"starred_at":"2002-02-10T15:30:00Z","repo":{"id":1}}]`)
	})

	repos, _, err := client.Activity.ListStarred(context.Background(), "", nil)
	if err != nil {
		t.Errorf("Activity.ListStarred returned error: %v", err)
	}

	want := []*StarredRepository{{StarredAt: &Timestamp{time.Date(2002, time.February, 10, 15, 30, 0, 0, time.UTC)}, Repository: &Repository{ID: Int64(1)}}}
	if !reflect.DeepEqual(repos, want) {
		t.Errorf("Activity.ListStarred returned %+v, want %+v", repos, want)
	}
}

func TestActivityService_ListStarred_specifiedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/users/u/starred", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeStarringPreview)
		testFormValues(t, r, values{
			"sort":      "created",
			"direction": "asc",
			"page":      "2",
		})
		fmt.Fprint(w, `[{"starred_at":"2002-02-10T15:30:00Z","repo":{"id":2}}]`)
	})

	opt := &ActivityListStarredOptions{"created", "asc", ListOptions{Page: 2}}
	repos, _, err := client.Activity.ListStarred(context.Background(), "u", opt)
	if err != nil {
		t.Errorf("Activity.ListStarred returned error: %v", err)
	}

	want := []*StarredRepository{{StarredAt: &Timestamp{time.Date(2002, time.February, 10, 15, 30, 0, 0, time.UTC)}, Repository: &Repository{ID: Int64(2)}}}
	if !reflect.DeepEqual(repos, want) {
		t.Errorf("Activity.ListStarred returned %+v, want %+v", repos, want)
	}
}

func TestActivityService_ListStarred_invalidUser(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Activity.ListStarred(context.Background(), "%", nil)
	testURLParseError(t, err)
}

func TestActivityService_IsStarred_hasStar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/starred/o/r", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.WriteHeader(http.StatusNoContent)
	})

	star, _, err := client.Activity.IsStarred(context.Background(), "o", "r")
	if err != nil {
		t.Errorf("Activity.IsStarred returned error: %v", err)
	}
	if want := true; star != want {
		t.Errorf("Activity.IsStarred returned %+v, want %+v", star, want)
	}
}

func TestActivityService_IsStarred_noStar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/starred/o/r", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.WriteHeader(http.StatusNotFound)
	})

	star, _, err := client.Activity.IsStarred(context.Background(), "o", "r")
	if err != nil {
		t.Errorf("Activity.IsStarred returned error: %v", err)
	}
	if want := false; star != want {
		t.Errorf("Activity.IsStarred returned %+v, want %+v", star, want)
	}
}

func TestActivityService_IsStarred_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Activity.IsStarred(context.Background(), "%", "%")
	testURLParseError(t, err)
}

func TestActivityService_Star(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/starred/o/r", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "PUT")
	})

	_, err := client.Activity.Star(context.Background(), "o", "r")
	if err != nil {
		t.Errorf("Activity.Star returned error: %v", err)
	}
}

func TestActivityService_Star_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Activity.Star(context.Background(), "%", "%")
	testURLParseError(t, err)
}

func TestActivityService_Unstar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/starred/o/r", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "DELETE")
	})

	_, err := client.Activity.Unstar(context.Background(), "o", "r")
	if err != nil {
		t.Errorf("Activity.Unstar returned error: %v", err)
	}
}

func TestActivityService_Unstar_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Activity.Unstar(context.Background(), "%", "%")
	testURLParseError(t, err)
}
