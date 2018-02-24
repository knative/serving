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
	"time"
)

func TestGistsService_List_specifiedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	since := "2013-01-01T00:00:00Z"

	mux.HandleFunc("/users/u/gists", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{
			"since": since,
		})
		fmt.Fprint(w, `[{"id": "1"}]`)
	})

	opt := &GistListOptions{Since: time.Date(2013, time.January, 1, 0, 0, 0, 0, time.UTC)}
	gists, _, err := client.Gists.List(context.Background(), "u", opt)
	if err != nil {
		t.Errorf("Gists.List returned error: %v", err)
	}

	want := []*Gist{{ID: String("1")}}
	if !reflect.DeepEqual(gists, want) {
		t.Errorf("Gists.List returned %+v, want %+v", gists, want)
	}
}

func TestGistsService_List_authenticatedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `[{"id": "1"}]`)
	})

	gists, _, err := client.Gists.List(context.Background(), "", nil)
	if err != nil {
		t.Errorf("Gists.List returned error: %v", err)
	}

	want := []*Gist{{ID: String("1")}}
	if !reflect.DeepEqual(gists, want) {
		t.Errorf("Gists.List returned %+v, want %+v", gists, want)
	}
}

func TestGistsService_List_invalidUser(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.List(context.Background(), "%", nil)
	testURLParseError(t, err)
}

func TestGistsService_ListAll(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	since := "2013-01-01T00:00:00Z"

	mux.HandleFunc("/gists/public", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{
			"since": since,
		})
		fmt.Fprint(w, `[{"id": "1"}]`)
	})

	opt := &GistListOptions{Since: time.Date(2013, time.January, 1, 0, 0, 0, 0, time.UTC)}
	gists, _, err := client.Gists.ListAll(context.Background(), opt)
	if err != nil {
		t.Errorf("Gists.ListAll returned error: %v", err)
	}

	want := []*Gist{{ID: String("1")}}
	if !reflect.DeepEqual(gists, want) {
		t.Errorf("Gists.ListAll returned %+v, want %+v", gists, want)
	}
}

func TestGistsService_ListStarred(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	since := "2013-01-01T00:00:00Z"

	mux.HandleFunc("/gists/starred", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{
			"since": since,
		})
		fmt.Fprint(w, `[{"id": "1"}]`)
	})

	opt := &GistListOptions{Since: time.Date(2013, time.January, 1, 0, 0, 0, 0, time.UTC)}
	gists, _, err := client.Gists.ListStarred(context.Background(), opt)
	if err != nil {
		t.Errorf("Gists.ListStarred returned error: %v", err)
	}

	want := []*Gist{{ID: String("1")}}
	if !reflect.DeepEqual(gists, want) {
		t.Errorf("Gists.ListStarred returned %+v, want %+v", gists, want)
	}
}

func TestGistsService_Get(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `{"id": "1"}`)
	})

	gist, _, err := client.Gists.Get(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Get returned error: %v", err)
	}

	want := &Gist{ID: String("1")}
	if !reflect.DeepEqual(gist, want) {
		t.Errorf("Gists.Get returned %+v, want %+v", gist, want)
	}
}

func TestGistsService_Get_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.Get(context.Background(), "%")
	testURLParseError(t, err)
}

func TestGistsService_GetRevision(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/s", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `{"id": "1"}`)
	})

	gist, _, err := client.Gists.GetRevision(context.Background(), "1", "s")
	if err != nil {
		t.Errorf("Gists.Get returned error: %v", err)
	}

	want := &Gist{ID: String("1")}
	if !reflect.DeepEqual(gist, want) {
		t.Errorf("Gists.Get returned %+v, want %+v", gist, want)
	}
}

func TestGistsService_GetRevision_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.GetRevision(context.Background(), "%", "%")
	testURLParseError(t, err)
}

func TestGistsService_Create(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &Gist{
		Description: String("Gist description"),
		Public:      Bool(false),
		Files: map[GistFilename]GistFile{
			"test.txt": {Content: String("Gist file content")},
		},
	}

	mux.HandleFunc("/gists", func(w http.ResponseWriter, r *http.Request) {
		v := new(Gist)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "POST")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w,
			`
			{
				"id": "1",
				"description": "Gist description",
				"public": false,
				"files": {
					"test.txt": {
						"filename": "test.txt"
					}
				}
			}`)
	})

	gist, _, err := client.Gists.Create(context.Background(), input)
	if err != nil {
		t.Errorf("Gists.Create returned error: %v", err)
	}

	want := &Gist{
		ID:          String("1"),
		Description: String("Gist description"),
		Public:      Bool(false),
		Files: map[GistFilename]GistFile{
			"test.txt": {Filename: String("test.txt")},
		},
	}
	if !reflect.DeepEqual(gist, want) {
		t.Errorf("Gists.Create returned %+v, want %+v", gist, want)
	}
}

func TestGistsService_Edit(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &Gist{
		Description: String("New description"),
		Files: map[GistFilename]GistFile{
			"new.txt": {Content: String("new file content")},
		},
	}

	mux.HandleFunc("/gists/1", func(w http.ResponseWriter, r *http.Request) {
		v := new(Gist)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "PATCH")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w,
			`
			{
				"id": "1",
				"description": "new description",
				"public": false,
				"files": {
					"test.txt": {
						"filename": "test.txt"
					},
					"new.txt": {
						"filename": "new.txt"
					}
				}
			}`)
	})

	gist, _, err := client.Gists.Edit(context.Background(), "1", input)
	if err != nil {
		t.Errorf("Gists.Edit returned error: %v", err)
	}

	want := &Gist{
		ID:          String("1"),
		Description: String("new description"),
		Public:      Bool(false),
		Files: map[GistFilename]GistFile{
			"test.txt": {Filename: String("test.txt")},
			"new.txt":  {Filename: String("new.txt")},
		},
	}
	if !reflect.DeepEqual(gist, want) {
		t.Errorf("Gists.Edit returned %+v, want %+v", gist, want)
	}
}

func TestGistsService_Edit_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.Edit(context.Background(), "%", nil)
	testURLParseError(t, err)
}

func TestGistsService_ListCommits(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/commits", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, nil)
		fmt.Fprint(w, `
		  [
		    {
		      "url": "https://api.github.com/gists/1/1",
		      "version": "1",
		      "user": {
		        "id": 1
		      },
		      "change_status": {
		        "deletions": 0,
		        "additions": 180,
		        "total": 180
		      },
		      "committed_at": "2010-01-01T00:00:00Z"
		    }
		  ]
		`)
	})

	gistCommits, _, err := client.Gists.ListCommits(context.Background(), "1", nil)
	if err != nil {
		t.Errorf("Gists.ListCommits returned error: %v", err)
	}

	want := []*GistCommit{{
		URL:         String("https://api.github.com/gists/1/1"),
		Version:     String("1"),
		User:        &User{ID: Int64(1)},
		CommittedAt: &Timestamp{time.Date(2010, 1, 1, 00, 00, 00, 0, time.UTC)},
		ChangeStatus: &CommitStats{
			Additions: Int(180),
			Deletions: Int(0),
			Total:     Int(180),
		}}}

	if !reflect.DeepEqual(gistCommits, want) {
		t.Errorf("Gists.ListCommits returned %+v, want %+v", gistCommits, want)
	}
}

func TestGistsService_ListCommits_withOptions(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/commits", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{
			"page": "2",
		})
		fmt.Fprint(w, `[]`)
	})

	_, _, err := client.Gists.ListCommits(context.Background(), "1", &ListOptions{Page: 2})
	if err != nil {
		t.Errorf("Gists.ListCommits returned error: %v", err)
	}
}

func TestGistsService_Delete(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "DELETE")
	})

	_, err := client.Gists.Delete(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Delete returned error: %v", err)
	}
}

func TestGistsService_Delete_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Gists.Delete(context.Background(), "%")
	testURLParseError(t, err)
}

func TestGistsService_Star(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/star", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "PUT")
	})

	_, err := client.Gists.Star(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Star returned error: %v", err)
	}
}

func TestGistsService_Star_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Gists.Star(context.Background(), "%")
	testURLParseError(t, err)
}

func TestGistsService_Unstar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/star", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "DELETE")
	})

	_, err := client.Gists.Unstar(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Unstar returned error: %v", err)
	}
}

func TestGistsService_Unstar_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, err := client.Gists.Unstar(context.Background(), "%")
	testURLParseError(t, err)
}

func TestGistsService_IsStarred_hasStar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/star", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.WriteHeader(http.StatusNoContent)
	})

	star, _, err := client.Gists.IsStarred(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Starred returned error: %v", err)
	}
	if want := true; star != want {
		t.Errorf("Gists.Starred returned %+v, want %+v", star, want)
	}
}

func TestGistsService_IsStarred_noStar(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/star", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.WriteHeader(http.StatusNotFound)
	})

	star, _, err := client.Gists.IsStarred(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Starred returned error: %v", err)
	}
	if want := false; star != want {
		t.Errorf("Gists.Starred returned %+v, want %+v", star, want)
	}
}

func TestGistsService_IsStarred_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.IsStarred(context.Background(), "%")
	testURLParseError(t, err)
}

func TestGistsService_Fork(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/forks", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "POST")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `{"id": "2"}`)
	})

	gist, _, err := client.Gists.Fork(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.Fork returned error: %v", err)
	}

	want := &Gist{ID: String("2")}
	if !reflect.DeepEqual(gist, want) {
		t.Errorf("Gists.Fork returned %+v, want %+v", gist, want)
	}
}

func TestGistsService_ListForks(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/gists/1/forks", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, nil)
		fmt.Fprint(w, `
		  [
		    {"url": "https://api.github.com/gists/1",
		     "user": {"id": 1},
		     "id": "1",
		     "created_at": "2010-01-01T00:00:00Z",
		     "updated_at": "2013-01-01T00:00:00Z"
		    }
		  ]
		`)
	})

	gistForks, _, err := client.Gists.ListForks(context.Background(), "1")
	if err != nil {
		t.Errorf("Gists.ListForks returned error: %v", err)
	}

	want := []*GistFork{{
		URL:       String("https://api.github.com/gists/1"),
		ID:        String("1"),
		User:      &User{ID: Int64(1)},
		CreatedAt: &Timestamp{time.Date(2010, 1, 1, 00, 00, 00, 0, time.UTC)},
		UpdatedAt: &Timestamp{time.Date(2013, 1, 1, 00, 00, 00, 0, time.UTC)}}}

	if !reflect.DeepEqual(gistForks, want) {
		t.Errorf("Gists.ListForks returned %+v, want %+v", gistForks, want)
	}
}

func TestGistsService_Fork_invalidID(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Gists.Fork(context.Background(), "%")
	testURLParseError(t, err)
}
