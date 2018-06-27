# Reviewing and Merging Pull Requests for Knative

As a community we believe in the value of code reviews for all contributions.
Code reviews increase both the quality and readability of our code base, which
in turn produces high quality software.

This document provides guidelines for how the project's
[Members](ROLES.md#member) review issues and merge pull requests (PRs).

*   [Pull requests welcome](#pull-requests-welcome)
*   [Code of Conduct](#code-of-conduct)
*   [Code reviewers](#code-reviewers)
*   [Reviewing changes](#reviewing-changes)
    *   [Holds](#holds)
*   [Project maintainers](#project-maintainers)
*   [Merging PRs](#merging-prs)

## Pull requests welcome

First and foremost: as a potential contributor, your changes and ideas are
welcome at any hour of the day or night, weekdays, weekends, and holidays.
Please do not ever hesitate to ask a question or send a PR.

## Code of Conduct

Because reviewers are often the first points of contact between new members of
the community and can therefore significantly impact the first impression of the
Knative community, reviewers are especially important in shaping the community.
Reviewers are highly encouraged to review the [code of
conduct](CODE-OF-CONDUCT.md) and are strongly encouraged to go above and beyond
the code of conduct to promote a collaborative and respectful community.

## Code reviewers

The code review process can introduce latency for contributors and additional
work for reviewers that can frustrate both parties. Consequently, as a community
we expect that all active participants in the community will also be active
reviewers. We ask that active contributors to the project participate in the
code review process in areas where that contributor has expertise.

## Reviewing changes

Once a PR has been submitted, reviewers should attempt to do an initial review
to do a quick "triage" (e.g. close duplicates, identify user errors, etc.), and
potentially identify which maintainers should be the focal points for the
review.

If a PR is closed without accepting the changes, reviewers are expected to
provide sufficient feedback to the originator to explain why it is being closed.

During a review, PR authors are expected to respond to comments and questions
made within the PR - updating the proposed change as appropriate.

After a review of the proposed changes, reviewers may either approve or reject
the PR. To approve they should add an "approved" review to the PR. To reject
they should add a "request changes" review along with a full justification for
why they are not in favor of the change. If a PR gets a "request changes" vote
then this issue should be discussed among the group to try to resolve their
differences.

Reviewers are expected to respond in a timely fashion to PRs that are assigned
to them. Reviewers are expected to respond to *active* PRs with reasonable
latency, and if reviewers fail to respond, those PRs may be assigned to other
reviewers. *Active* PRs are considered those which have a proper CLA (`cla:yes`)
label, are not WIP, are passing tests, and do not need rebase to be merged. PRs
that do not have a proper CLA, are WIP, do not pass tests, or require a rebase
are not considered active PRs.

### Holds

Any [Approver](ROLES.md#approver) who wants to review a PR but does not have
time immediately may put a hold on a PR simply by saying so on the PR discussion
and offering an ETA measured in single-digit days at most. Any PR that has a
hold shall not be merged until the person who requested the hold acks the
review, withdraws their hold, or is overruled by a preponderance of approvers.

## Approvers

Merging of PRs is done by [Approvers](ROLES.md#approver).

Like many open source projects, becoming an Approver is based on contributions
to the project. Please see our [community roles](ROLES.md) document for
information on how this is done.

## Merging PRs

PRs may only be merged after the following criteria are met:

1.  It has no "request changes" review from a reviewer.
1.  It has at least one "approved" review by at least one of the approvers of
    that repository.
1.  It has all appropriate corresponding documentation and tests.

## Prow

This project uses
[Prow](https://github.com/kubernetes/test-infra/tree/master/prow) to
automatically run tests for every PR. PRs with failing tests may not be merged.
If necessary, you can rerun the tests by simply adding the comment `/retest` to
your PR.

Prow has several other features that make PR management easier, like running the
go linter or assigning labels. A full list of commands understood by Prow can be
found in the [command help
page](https://prow-internal.gcpnode.com/command-help?repo=knative%2Fknative).

### Viewing test logs

Currently the Prow instance is internal to Google, which means that only Google
employees are able to access the "Details" link of the test job (provided by
Prow in the PR thread).

However, if you're an Knative team member outside Google, and provided that you
are a member of the [knative-dev@](https://groups.google.com/forum/#!forum/knative-dev)
Google group, you can see the test logs by following these instructions:

1. Wait for prow to finish the test execution. Note down the PR number.

2. Open the URL http://gcsweb.k8s.io/gcs/ela-prow/pr-logs/pull/knative_serving/###/pull-knative-serving-@@@-tests/
where ### is the PR number and @@@ the test type (_build_, _unit_ or _integration_).

3. You'll see one or more numbered directories, the highest number is the latest
test execution (called "build" by Prow).

4. The raw test log is the text file named `build-log.txt` inside each numbered
directory.
