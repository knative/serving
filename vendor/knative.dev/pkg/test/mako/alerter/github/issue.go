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

package github

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/go-github/github"

	"knative.dev/pkg/test/ghutil"
	"knative.dev/pkg/test/helpers"
)

const (
	// perfLabel is the Github issue label used for querying all auto-generated performance issues.
	perfLabel = "auto:perf"
	// number of days for an issue to be considered old
	daysConsideredOld = 30

	// number of days for an issue to be considered active
	// To avoid frequent open/close actions, only automatically close an issue if there is no activity
	// (update, comment, etc.) on it for a specified time
	daysConsideredActive = 3

	// issueTitleTemplate is a template for issue title
	issueTitleTemplate = "[performance] %s"

	// issueBodyTemplate is a template for issue body
	issueBodyTemplate = `
### Auto-generated issue tracking performance regression
* **Repository name**: %s
* **Test name**: %s`

	// issueSummaryCommentTemplate is a template for the summary of an issue
	issueSummaryCommentTemplate = `
A new regression for this test has been detected:
%s`

	// reopenIssueComment is the comment of an issue when it is reopened
	reopenIssueComment = `
New regression has been detected, reopening this issue.`

	// closeIssueComment is the comment of an issue when it is closed
	closeIssueComment = `
The performance regression goes away for this test, closing this issue.`
)

// IssueHandler handles methods for github issues
type IssueHandler struct {
	client ghutil.GithubOperations
	config config
}

// config is the global config that can be used in Github operations
type config struct {
	org    string
	repo   string
	dryrun bool
}

// Setup creates the necessary setup to make calls to work with github issues
func Setup(org, repo, githubTokenPath string, dryrun bool) (*IssueHandler, error) {
	if org == "" {
		return nil, errors.New("org cannot be empty")
	}
	if repo == "" {
		return nil, errors.New("repo cannot be empty")
	}
	ghc, err := ghutil.NewGithubClient(githubTokenPath)
	if err != nil {
		return nil, fmt.Errorf("cannot authenticate to github: %w", err)
	}
	conf := config{org: org, repo: repo, dryrun: dryrun}
	return &IssueHandler{client: ghc, config: conf}, nil
}

// CreateIssueForTest will try to add an issue with the given testName and description.
// If there is already an issue related to the test, it will try to update that issue.
func (gih *IssueHandler) CreateIssueForTest(testName, desc string) error {
	title := fmt.Sprintf(issueTitleTemplate, testName)
	issue, err := gih.findIssue(title)
	if err != nil {
		return fmt.Errorf("failed to find issues for test %q: %w, skipped creating new issue", testName, err)
	}
	// If the issue hasn't been created, create one
	if issue == nil {
		commentBody := fmt.Sprintf(issueBodyTemplate, gih.config.repo, testName)
		issue, err := gih.createNewIssue(title, commentBody)
		if err != nil {
			return fmt.Errorf("failed to create a new issue for test %q: %w", testName, err)
		}
		commentBody = fmt.Sprintf(issueSummaryCommentTemplate, desc)
		if err := gih.addComment(*issue.Number, commentBody); err != nil {
			return fmt.Errorf("failed to add comment for new issue %d: %w", *issue.Number, err)
		}
		return nil
	}

	// If the issue has been created, edit it
	issueNumber := *issue.Number

	// If the issue has been closed, reopen it
	if *issue.State == string(ghutil.IssueCloseState) {
		if err := gih.reopenIssue(issueNumber); err != nil {
			return fmt.Errorf("failed to reopen issue %d: %w", issueNumber, err)
		}
		if err := gih.addComment(issueNumber, reopenIssueComment); err != nil {
			return fmt.Errorf("failed to add comment for reopened issue %d: %w", issueNumber, err)
		}
	}

	// Edit the old comment
	comments, err := gih.getComments(issueNumber)
	if err != nil {
		return fmt.Errorf("failed to get comments from issue %d: %w", issueNumber, err)
	}
	if len(comments) < 1 {
		return fmt.Errorf("existing issue %d is malformed, cannot update", issueNumber)
	}
	commentBody := fmt.Sprintf(issueSummaryCommentTemplate, desc)
	if err := gih.editComment(issueNumber, *comments[0].ID, commentBody); err != nil {
		return fmt.Errorf("failed to edit the comment for issue %d: %w", issueNumber, err)
	}
	return nil
}

// createNewIssue will create a new issue, and add perfLabel for it.
func (gih *IssueHandler) createNewIssue(title, body string) (*github.Issue, error) {
	var newIssue *github.Issue
	if err := helpers.Run(
		fmt.Sprintf("creating issue %q in %q", title, gih.config.repo),
		func() error {
			var err error
			newIssue, err = gih.client.CreateIssue(gih.config.org, gih.config.repo, title, body)
			return err
		},
		gih.config.dryrun,
	); nil != err {
		return nil, err
	}
	if err := helpers.Run(
		fmt.Sprintf("adding perf label for issue %q in %q", title, gih.config.repo),
		func() error {
			return gih.client.AddLabelsToIssue(gih.config.org, gih.config.repo, *newIssue.Number, []string{perfLabel})
		},
		gih.config.dryrun,
	); nil != err {
		return nil, err
	}
	return newIssue, nil
}

// CloseIssueForTest will try to close the issue for the given testName.
// If there is no issue related to the test or the issue is already closed, the function will do nothing.
func (gih *IssueHandler) CloseIssueForTest(testName string) error {
	title := fmt.Sprintf(issueTitleTemplate, testName)
	issue, err := gih.findIssue(title)
	// If no issue has been found, or the issue has already been closed, do nothing.
	if issue == nil || err != nil || *issue.State == string(ghutil.IssueCloseState) {
		return nil
	}
	// If the issue is still active, do not close it.
	if time.Since(issue.GetUpdatedAt()) < daysConsideredActive*24*time.Hour {
		return nil
	}

	issueNumber := *issue.Number
	if err := gih.addComment(issueNumber, closeIssueComment); err != nil {
		return fmt.Errorf("failed to add comment for the issue %d to close: %w", issueNumber, err)
	}
	if err := gih.closeIssue(issueNumber); err != nil {
		return fmt.Errorf("failed to close the issue %d: %w", issueNumber, err)
	}
	return nil
}

// reopenIssue will reopen the given issue.
func (gih *IssueHandler) reopenIssue(issueNumber int) error {
	return helpers.Run(
		fmt.Sprintf("reopening issue %d in %q", issueNumber, gih.config.repo),
		func() error {
			return gih.client.ReopenIssue(gih.config.org, gih.config.repo, issueNumber)
		},
		gih.config.dryrun,
	)
}

// closeIssue will close the given issue.
func (gih *IssueHandler) closeIssue(issueNumber int) error {
	return helpers.Run(
		fmt.Sprintf("closing issue %d in %q", issueNumber, gih.config.repo),
		func() error {
			return gih.client.CloseIssue(gih.config.org, gih.config.repo, issueNumber)
		},
		gih.config.dryrun,
	)
}

// findIssue will return the issue in the given repo if it exists.
func (gih *IssueHandler) findIssue(title string) (*github.Issue, error) {
	var issues []*github.Issue
	if err := helpers.Run(
		fmt.Sprintf("listing issues in %q", gih.config.repo),
		func() error {
			var err error
			issues, err = gih.client.ListIssuesByRepo(gih.config.org, gih.config.repo, []string{perfLabel})
			return err
		},
		gih.config.dryrun,
	); err != nil {
		return nil, err
	}

	var existingIssue *github.Issue
	for _, issue := range issues {
		if *issue.Title == title {
			// If the issue has been closed a long time ago, ignore this issue.
			if issue.GetState() == string(ghutil.IssueCloseState) &&
				time.Since(*issue.UpdatedAt) > daysConsideredOld*24*time.Hour {
				continue
			}

			// If there are multiple issues, return the one that was created most recently.
			if existingIssue == nil || issue.CreatedAt.After(*existingIssue.CreatedAt) {
				existingIssue = issue
			}
		}
	}

	return existingIssue, nil
}

// getComments will get comments for the given issue.
func (gih *IssueHandler) getComments(issueNumber int) ([]*github.IssueComment, error) {
	var comments []*github.IssueComment
	if err := helpers.Run(
		fmt.Sprintf("getting comments for issue %d in %q", issueNumber, gih.config.repo),
		func() error {
			var err error
			comments, err = gih.client.ListComments(gih.config.org, gih.config.repo, issueNumber)
			return err
		},
		gih.config.dryrun,
	); err != nil {
		return comments, err
	}
	return comments, nil
}

// addComment will add comment for the given issue.
func (gih *IssueHandler) addComment(issueNumber int, commentBody string) error {
	return helpers.Run(
		fmt.Sprintf("adding comment %q for issue %d in %q", commentBody, issueNumber, gih.config.repo),
		func() error {
			_, err := gih.client.CreateComment(gih.config.org, gih.config.repo, issueNumber, commentBody)
			return err
		},
		gih.config.dryrun,
	)
}

// editComment will edit the comment to the new body.
func (gih *IssueHandler) editComment(issueNumber int, commentID int64, commentBody string) error {
	return helpers.Run(
		fmt.Sprintf("editting comment to %q for issue %d in %q", commentBody, issueNumber, gih.config.repo),
		func() error {
			return gih.client.EditComment(gih.config.org, gih.config.repo, commentID, commentBody)
		},
		gih.config.dryrun,
	)
}
