/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package issue

import (
	"fmt"
	"time"

	"github.com/google/go-github/github"

	"knative.dev/test-infra/shared/ghutil"
)

const (
	githubToken = "/var/github-token/token"
	// perfLabel is the Github issue label used for querying all auto-generated performance issues.
	perfLabel       = "auto:perf"
	daysConsiderOld = 10 // arbitrary number of days for an issue to be considered old

	// issueTitleTemplate is a template for issue title
	issueTitleTemplate = "[performance] %s"

	// issueBodyTemplate is a template for issue body
	issueBodyTemplate = `
### Auto-generated issue tracking performance regression
* **Test name**: %s
* **Summary**: %s`

	// reopenIssueCommentTemplate is a template for the comment of an issue that is reopened
	reopenIssueCommentTemplate = `
New regression has been detected, reopening this issue:
%s	
	`

	// newIssueCommentTemplate is a template for the comment of an issue that has been quiet for a long time
	newIssueCommentTemplate = `
A new regression related for this issue has been detected:
%s
	`
)

// GithubIssueHandler handles methods for github issues
type GithubIssueHandler struct {
	user   *github.User
	client ghutil.GithubOperations
}

// Setup creates the necessary setup to make calls to work with github issues
func Setup() (*GithubIssueHandler, error) {
	ghc, err := ghutil.NewGithubClient(githubToken)
	if err != nil {
		return nil, fmt.Errorf("Cannot authenticate to github: %v", err)
	}

	ghUser, err := ghc.GetGithubUser()
	if nil != err {
		return nil, fmt.Errorf("Cannot get username: %v", err)
	}
	return &GithubIssueHandler{user: ghUser, client: ghc}, nil
}

// AddIssue will work as the following logic:
// 1. If the issue hasn't been created, create one
// 2. If the issue has been created:
//    i. If it's closed, reopen it and add new comment
//   ii. If it's not closed and the last modification time is 5 days ago, add a new comment
//  iii. Otherwise do nothing to this issue
func (gih *GithubIssueHandler) AddIssue(org, repoForIssue, testName, desc string, dryrun bool) error {
	title := fmt.Sprintf(issueTitleTemplate, testName)
	issue := gih.findIssue(org, repoForIssue, title, dryrun)
	if issue == nil {
		body := fmt.Sprintf(issueBodyTemplate, testName, desc)
		return gih.createNewIssue(org, repoForIssue, title, body, dryrun)
	}

	if *issue.State == string(ghutil.IssueCloseState) {
		if err := gih.reopenIssue(org, repoForIssue, *issue.Number, dryrun); err != nil {
			return err
		}
		comment := fmt.Sprintf(reopenIssueCommentTemplate, desc)
		if err := gih.addComment(org, repoForIssue, *issue.Number, comment, dryrun); err != nil {
			return err
		}
	} else {
		if time.Now().Sub(*issue.UpdatedAt) > daysConsiderOld*24*time.Hour {
			comment := fmt.Sprintf(newIssueCommentTemplate, desc)
			if err := gih.addComment(org, repoForIssue, *issue.Number, comment, dryrun); err != nil {
				return err
			}
		}
	}

	return nil
}

// createNewIssue will create a new issue, and add perfLabel for it.
func (gih *GithubIssueHandler) createNewIssue(org, repoForIssue, title, body string, dryrun bool) error {
	var newIssue *github.Issue
	if err := run(
		"creating issue",
		func() error {
			var err error
			newIssue, err = gih.client.CreateIssue(org, repoForIssue, title, body)
			return err
		},
		dryrun,
	); nil != err {
		return fmt.Errorf("failed creating issue '%s' in repo '%s'", title, repoForIssue)
	}
	if err := run(
		"adding perf label",
		func() error {
			return gih.client.AddLabelsToIssue(org, repoForIssue, *newIssue.Number, []string{perfLabel})
		},
		dryrun,
	); nil != err {
		return fmt.Errorf("failed adding perf label for issue '%s' in repo '%s'", title, repoForIssue)
	}
	return nil
}

// CloseIssue will remove the perfLabel, and close the issue.
func (gih *GithubIssueHandler) CloseIssue(org, repoForIssue string, issueNumber int, dryrun bool) error {
	return run(
		"cleaning up invalid issue",
		func() error {
			var gErr []error
			if rlErr := gih.client.RemoveLabelForIssue(org, repoForIssue, issueNumber, perfLabel); nil != rlErr {
				gErr = append(gErr, rlErr)
			}
			if cErr := gih.client.CloseIssue(org, repoForIssue, issueNumber); nil != cErr {
				gErr = append(gErr, cErr)
			}
			return combineErrors(gErr)
		},
		dryrun,
	)
}

// reopenIssue will reopen the given issue.
func (gih *GithubIssueHandler) reopenIssue(org, repoForIssue string, issueNumber int, dryrun bool) error {
	return run(
		"reopen the issue",
		func() error {
			return gih.client.ReopenIssue(org, repoForIssue, issueNumber)
		},
		dryrun,
	)
}

// findIssue will return the issue in the given repo if it exists.
func (gih *GithubIssueHandler) findIssue(org, repoForIssue, title string, dryrun bool) *github.Issue {
	var issues []*github.Issue
	run(
		"list issues in the repo",
		func() error {
			var err error
			issues, err = gih.client.ListIssuesByRepo(org, repoForIssue, []string{perfLabel})
			return err
		},
		dryrun,
	)
	for _, issue := range issues {
		if *issue.Title == title {
			return issue
		}
	}
	return nil
}

// addComment will add comment for the given issue.
func (gih *GithubIssueHandler) addComment(org, repoForIssue string, issueNumber int, commentBody string, dryrun bool) error {
	return run(
		"add comment for issue",
		func() error {
			_, err := gih.client.CreateComment(org, repoForIssue, issueNumber, commentBody)
			return err
		},
		dryrun,
	)
}
