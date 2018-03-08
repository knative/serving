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

func TestOrganizationsService_ListAll(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	since := 1342004
	mux.HandleFunc("/organizations", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{"since": "1342004"})
		fmt.Fprint(w, `[{"id":4314092}]`)
	})

	opt := &OrganizationsListOptions{Since: since}
	orgs, _, err := client.Organizations.ListAll(context.Background(), opt)
	if err != nil {
		t.Errorf("Organizations.ListAll returned error: %v", err)
	}

	want := []*Organization{{ID: Int64(4314092)}}
	if !reflect.DeepEqual(orgs, want) {
		t.Errorf("Organizations.ListAll returned %+v, want %+v", orgs, want)
	}
}

func TestOrganizationsService_List_authenticatedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `[{"id":1},{"id":2}]`)
	})

	orgs, _, err := client.Organizations.List(context.Background(), "", nil)
	if err != nil {
		t.Errorf("Organizations.List returned error: %v", err)
	}

	want := []*Organization{{ID: Int64(1)}, {ID: Int64(2)}}
	if !reflect.DeepEqual(orgs, want) {
		t.Errorf("Organizations.List returned %+v, want %+v", orgs, want)
	}
}

func TestOrganizationsService_List_specifiedUser(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/users/u/orgs", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		testFormValues(t, r, values{"page": "2"})
		fmt.Fprint(w, `[{"id":1},{"id":2}]`)
	})

	opt := &ListOptions{Page: 2}
	orgs, _, err := client.Organizations.List(context.Background(), "u", opt)
	if err != nil {
		t.Errorf("Organizations.List returned error: %v", err)
	}

	want := []*Organization{{ID: Int64(1)}, {ID: Int64(2)}}
	if !reflect.DeepEqual(orgs, want) {
		t.Errorf("Organizations.List returned %+v, want %+v", orgs, want)
	}
}

func TestOrganizationsService_List_invalidUser(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Organizations.List(context.Background(), "%", nil)
	testURLParseError(t, err)
}

func TestOrganizationsService_Get(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/orgs/o", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `{"id":1, "login":"l", "url":"u", "avatar_url": "a", "location":"l"}`)
	})

	org, _, err := client.Organizations.Get(context.Background(), "o")
	if err != nil {
		t.Errorf("Organizations.Get returned error: %v", err)
	}

	want := &Organization{ID: Int64(1), Login: String("l"), URL: String("u"), AvatarURL: String("a"), Location: String("l")}
	if !reflect.DeepEqual(org, want) {
		t.Errorf("Organizations.Get returned %+v, want %+v", org, want)
	}
}

func TestOrganizationsService_Get_invalidOrg(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Organizations.Get(context.Background(), "%")
	testURLParseError(t, err)
}

func TestOrganizationsService_GetByID(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/organizations/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		fmt.Fprint(w, `{"id":1, "login":"l", "url":"u", "avatar_url": "a", "location":"l"}`)
	})

	org, _, err := client.Organizations.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("Organizations.GetByID returned error: %v", err)
	}

	want := &Organization{ID: Int64(1), Login: String("l"), URL: String("u"), AvatarURL: String("a"), Location: String("l")}
	if !reflect.DeepEqual(org, want) {
		t.Errorf("Organizations.GetByID returned %+v, want %+v", org, want)
	}
}

func TestOrganizationsService_Edit(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := &Organization{Login: String("l")}

	mux.HandleFunc("/orgs/o", func(w http.ResponseWriter, r *http.Request) {
		v := new(Organization)
		json.NewDecoder(r.Body).Decode(v)

		testMethod(t, r, "PATCH")
		testHeader(t, r, "Accept", mediaTypeGraphQLNodeIDPreview)
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w, `{"id":1}`)
	})

	org, _, err := client.Organizations.Edit(context.Background(), "o", input)
	if err != nil {
		t.Errorf("Organizations.Edit returned error: %v", err)
	}

	want := &Organization{ID: Int64(1)}
	if !reflect.DeepEqual(org, want) {
		t.Errorf("Organizations.Edit returned %+v, want %+v", org, want)
	}
}

func TestOrganizationsService_Edit_invalidOrg(t *testing.T) {
	client, _, _, teardown := setup()
	defer teardown()

	_, _, err := client.Organizations.Edit(context.Background(), "%", nil)
	testURLParseError(t, err)
}
