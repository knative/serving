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

func TestUsersService_ListEmails(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		testFormValues(t, r, values{"page": "2"})
		fmt.Fprint(w, `[{
			"email": "user@example.com",
			"verified": false,
			"primary": true
		}]`)
	})

	opt := &ListOptions{Page: 2}
	emails, _, err := client.Users.ListEmails(context.Background(), opt)
	if err != nil {
		t.Errorf("Users.ListEmails returned error: %v", err)
	}

	want := []*UserEmail{{Email: String("user@example.com"), Verified: Bool(false), Primary: Bool(true)}}
	if !reflect.DeepEqual(emails, want) {
		t.Errorf("Users.ListEmails returned %+v, want %+v", emails, want)
	}
}

func TestUsersService_AddEmails(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := []string{"new@example.com"}

	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		var v []string
		json.NewDecoder(r.Body).Decode(&v)

		testMethod(t, r, "POST")
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}

		fmt.Fprint(w, `[{"email":"old@example.com"}, {"email":"new@example.com"}]`)
	})

	emails, _, err := client.Users.AddEmails(context.Background(), input)
	if err != nil {
		t.Errorf("Users.AddEmails returned error: %v", err)
	}

	want := []*UserEmail{
		{Email: String("old@example.com")},
		{Email: String("new@example.com")},
	}
	if !reflect.DeepEqual(emails, want) {
		t.Errorf("Users.AddEmails returned %+v, want %+v", emails, want)
	}
}

func TestUsersService_DeleteEmails(t *testing.T) {
	client, mux, _, teardown := setup()
	defer teardown()

	input := []string{"user@example.com"}

	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		var v []string
		json.NewDecoder(r.Body).Decode(&v)

		testMethod(t, r, "DELETE")
		if !reflect.DeepEqual(v, input) {
			t.Errorf("Request body = %+v, want %+v", v, input)
		}
	})

	_, err := client.Users.DeleteEmails(context.Background(), input)
	if err != nil {
		t.Errorf("Users.DeleteEmails returned error: %v", err)
	}
}
