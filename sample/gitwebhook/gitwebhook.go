/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/oauth2"
	webhooks "gopkg.in/go-playground/webhooks.v3"
	"gopkg.in/go-playground/webhooks.v3/github"

	ghclient "github.com/google/go-github/github"
)

const (
	// Secret given to github. Used for verifying the incoming objects.
	accessTokenKey = "ACCESS_TOKEN"
	// Personal Access Token created in github that allows us to make
	// calls into github.
	secretTokenKey = "SECRET_TOKEN"
	// this is what we tack onto each PR title if not there already
	titleSuffix = "looks pretty legit"
)

// GithubHandler holds necessary objects for communicating with the Github.
type GithubHandler struct {
	client *ghclient.Client
	ctx    context.Context
}

// HandlePullRequest is invoked whenever a PullRequest is modified (created, updated, etc.)
func (handler *GithubHandler) HandlePullRequest(payload interface{}, header webhooks.Header) {
	log.Print("Handling Pull Request")

	pl := payload.(github.PullRequestPayload)

	// Do whatever you want from here...
	title := pl.PullRequest.Title
	log.Printf("GOT PR with Title: %q", title)

	// Check the title and if it contains 'looks pretty legit' leave it alone
	if strings.Contains(title, titleSuffix) {
		// already modified, leave it alone.
		return
	}

	newTitle := fmt.Sprintf("%s (%s)", title, titleSuffix)
	updatedPR := ghclient.PullRequest{
		Title: &newTitle,
	}
	newPR, response, err := handler.client.PullRequests.Edit(handler.ctx, pl.Repository.Owner.Login, pl.Repository.Name, int(pl.Number), &updatedPR)
	if err != nil {
		log.Printf("Failed to update PR: %s\n%s", err, response)
		return
	}
	if newPR.Title != nil {
		log.Printf("New PR Title: %q", *newPR.Title)
	} else {
		log.Print("New PR title is nil")
	}
}

func main() {
	flag.Parse()
	log.Print("gitwebhook sample started.")
	accessToken := os.Getenv(accessTokenKey)
	secretToken := os.Getenv(secretTokenKey)

	// Set up the auth for being able to talk to Github. It's
	// odd that you have to also pass context around for the
	// calls even after giving it to client. But, whatever.
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := ghclient.NewClient(tc)

	h := &GithubHandler{
		client: client,
		ctx:    ctx,
	}

	hook := github.New(&github.Config{Secret: secretToken})
	hook.RegisterEvents(h.HandlePullRequest, github.PullRequestEvent)

	err := webhooks.Run(hook, ":8080", "/")
	if err != nil {
		fmt.Println("Failed to run the webhook")
	}
}
