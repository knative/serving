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

package alerter

import (
	qpb "github.com/google/mako/proto/quickstore/quickstore_go_proto"
	"knative.dev/pkg/test/mako/alerter/github"
	"knative.dev/pkg/test/mako/alerter/slack"
)

// Alerter controls alert for performance regressions detected by Mako.
type Alerter struct {
	githubIssueHandler  *github.IssueHandler
	slackMessageHandler *slack.MessageHandler
}

// SetupGitHub will setup SetupGitHub for the alerter.
func (alerter *Alerter) SetupGitHub(org, repo, githubTokenPath string) error {
	issueHandler, err := github.Setup(org, repo, githubTokenPath, false)
	if err != nil {
		return err
	}
	alerter.githubIssueHandler = issueHandler
	return nil
}

// SetupSlack will setup Slack for the alerter.
func (alerter *Alerter) SetupSlack(repo, userName, readTokenPath, writeTokenPath string) error {
	messageHandler, err := slack.Setup(userName, readTokenPath, writeTokenPath, repo, false)
	if err != nil {
		return err
	}
	alerter.slackMessageHandler = messageHandler
	return nil
}

// HandleBenchmarkResult will handle the benchmark result which returns from `q.Store()`
func (alerter *Alerter) HandleBenchmarkResult(testName string, output qpb.QuickstoreOutput, err error) {
	if err != nil {
		if output.GetStatus() == qpb.QuickstoreOutput_ANALYSIS_FAIL {
			summary := output.GetSummaryOutput()
			if alerter.githubIssueHandler != nil {
				alerter.githubIssueHandler.CreateIssueForTest(testName, summary)
			}
			if alerter.slackMessageHandler != nil {
				alerter.slackMessageHandler.SendAlert(summary)
			}
		}
		return
	}
	if alerter.githubIssueHandler != nil {
		alerter.githubIssueHandler.CloseIssueForTest(testName)
	}
}
