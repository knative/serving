/*
Copyright 2018 Google LLC

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
	"os"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/oauth2"
	webhooks "gopkg.in/go-playground/webhooks.v3"
	"gopkg.in/go-playground/webhooks.v3/github"

	ghclient "github.com/google/go-github/github"
)

const (
	accessTokenKey = "ACCESS_TOKEN"
	secretTokenKey = "SECRET_TOKEN"
)

type GithubHandler struct {
	accessToken string
	secretToken string
	hook        *github.Webhook
	client      *ghclient.Client
	ctx         context.Context
}

func NewGithubHandler(accessToken string, secretToken string, hook *github.Webhook, client *ghclient.Client, context context.Context) *GithubHandler {
	return &GithubHandler{
		accessToken: accessToken,
		secretToken: secretToken,
		hook:        hook,
		client:      client,
		ctx:         context,
	}
}

func (handler *GithubHandler) HandlePullRequest(payload interface{}, header webhooks.Header) {
	glog.Info("Handling Pull Request")

	pl := payload.(github.PullRequestPayload)

	// Do whatever you want from here...
	glog.Infof("GOT PR:\n%+v", pl)

	// Check the title and if it contains 'looks pretty legit' leave it alone
	if strings.Contains(pl.PullRequest.Title, "looks pretty legit") {
		// already modified, call it good...
		return
	}

	s := fmt.Sprintf("%s (%s)", pl.PullRequest.Title, "looks pretty legit")
	updatedPR := ghclient.PullRequest{
		Title: &s,
	}
	newPR, response, err := handler.client.PullRequests.Edit(handler.ctx, pl.Repository.Owner.Login, pl.Repository.Name, int(pl.Number), &updatedPR)
	if err != nil {
		glog.Warningf("Failed to update PR: %s", err)
		return
	}
	glog.Infof("New PR: %v\nresponse: %s", newPR, response)
}

func (handler *GithubHandler) HandleLabelRequest(payload interface{}, header webhooks.Header) {
	glog.Info("Handling Label Request")

	pl := payload.(github.LabelPayload)

	// Do whatever you want from here...
	glog.Infof("GOT Label Request:\n%+v", pl)
	glog.Infof("Action is: %q", pl.Action)
}

func main() {
	flag.Parse()
	// set the logs to stderr so kube will see them.
	flag.Lookup("logtostderr").Value.Set("true")
	accessToken := os.Getenv(accessTokenKey)
	secretToken := os.Getenv(secretTokenKey)
	hook := github.New(&github.Config{Secret: secretToken})

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := ghclient.NewClient(tc)

	h := NewGithubHandler(accessToken, secretToken, hook, client, ctx)
	hook.RegisterEvents(h.HandlePullRequest, github.PullRequestEvent)
	hook.RegisterEvents(h.HandleLabelRequest, github.LabelEvent)

	err := webhooks.Run(hook, ":8080", "/")
	if err != nil {
		fmt.Println("Failed to run the webhook")
	}
}
