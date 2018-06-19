# Contributing to Knative

So, you want to hack on Knative? Yay!

The following sections outline the process all changes to the Knative
repositories go through. All changes, regardless of whether they are from
newcomers to the community or from the core team follow the same process and
are given the same level of review.

*   [Working groups](#working-groups)
*   [Code of conduct](#code-of-conduct)
*   [Team values](#team-values)
*   [Contributor license agreements](#contributor-license-agreements)
*   [Design documents](#design-documents)
*   [Contributing a feature](#contributing-a-feature)
*   [Setting up to contribute to Knative](#setting-up-to-contribute-to-knative)
*   [Pull requests](#pull-requests)
*   [Issues](#issues)

## Working groups

The Knative community is organized into a set of [working
groups](WORKING-GROUPS.md). Any contribution to Knative should be started by
first engaging with the appropriate working group.

## Code of conduct

All members of the Knative community must abide by the [Code of
Conduct](CODE-OF-CONDUCT.md). Only by respecting each other can we develop a
productive, collaborative community.

## Team values

We promote and encourage a set of [shared values](VALUES.md) to improve our
productivity and inter-personal interactions.

## Contributor license agreements

We'd love to accept your patches! But before we can take them, you will have to
fill out the [Google CLA](https://cla.developers.google.com).

Once you are CLA'ed, we'll be able to accept your pull requests. This is
necessary because you own the copyright to your changes, even after your
contribution becomes part of this project. So this agreement simply gives us
permission to use and redistribute your contributions as part of the project.

## Design documents

Any substantial design deserves a design document. Design documents are
written with Google Docs and should be shared with the community by adding
the doc to our
[Team Drive](https://drive.google.com/corp/drive/folders/0APnJ_hRs30R2Uk9PVA)
and sending an email to the appropriate working group's mailing list to let
people know the doc is there. To get write access to the drive, you'll need
to be a [member](ROLES.md#member) of the Knative organization.

We do not yet have a common design document template(TODO).

The team drive is shared with the
[knative-dev@](https://groups.google.com/forum/#!forum/knative-dev) Google
group for editing and commenting. If you're not part of that group, the
first time you try to access the team drive or a specific doc, you'll be
asked to request permission. This permission will always be granted and we
do our best to grant access as fast as we can, but there is a human involved
there, so please forgive any delays.

## Contributing a feature

In order to contribute a feature to Knative you'll need to go through the
following steps:

*   Discuss your idea with the appropriate [working groups](WORKING-GROUPS.md)
    on the working group's mailing list.

*   Once there is general agreement that the feature is useful, create a GitHub
    issue to track the discussion. The issue should include information about
    the requirements and use cases that it is trying to address. Include a
    discussion of the proposed design and technical details of the
    implementation in the issue.

*   If the feature is substantial enough:

    *   Working group leads will ask for a design document as outlined in
        [design documents](#design-documents). Create the design document and
        add a link to it in the GitHub issue. Don't forget to send a note to the
        working group to let everyone know your document is ready for review.

    *   Depending on the breadth of the design and how contentious it is, the
        working group leads may decide the feature needs to be discussed in one
        or more working group meetings before being approved.

    *   Once the major technical issues are resolved and agreed upon, post a
        note with the design decision and the general execution plan to the
        working group's mailing list and on the feature's issue.

*   Submit PRs to knative/serving with your code changes.

*   Submit PRs to knative/serving with user documentation for your feature,
    including usage examples when possible.
    <!-- TODO: switch to knative/serving.dev) -->

*Note that we prefer bite-sized PRs instead of giant monster PRs. It's therefore
preferable if you can introduce large features in small, individually-reviewable
PRs that build on top of one another.*

If you would like to skip the process of submitting an issue and instead would
prefer to just submit a pull request with your desired code changes then that's
fine. But keep in mind that there is no guarantee of it being accepted and so it
is usually best to get agreement on the idea/design before time is spent coding
it. However, sometimes seeing the exact code change can help focus discussions,
so the choice is up to you.

## Setting up to contribute to Knative

Check out this
[README](https://github.com/knative/serving/blob/master/README.md) to learn
about the Knative source base and setting up your [development
environment](https://github.com/knative/serving/blob/master/DEVELOPMENT.md).

## Pull requests

If you're working on an existing issue, simply respond to the issue and express
interest in working on it. This helps other people know that the issue is
active, and hopefully prevents duplicated efforts.

To submit a proposed change:

*   Fork the affected repository.
*   Create a new branch for your changes.
*   Develop the code/fix.
*   Add new test cases. In the case of a bug fix, the tests should fail without
    your code changes. For new features try to cover as many variants as
    reasonably possible.
*   Modify the documentation as necessary.
*   Verify all CI status checks pass, and work to make them pass if failing.

The general rule is that all PRs should be 100% complete - meaning they should
include all test cases and documentation changes related to the change. A
significant exception is work-in-progress PRs. These should be indicated by a
`[WIP]` prefix in the PR title. WIP PRs should not be merged as long as they are
marked WIP.

When ready, if you have not already done so, sign a [contributor license
agreement](#contributor-license-agreements) and submit the PR.

This project uses [Prow](https://github.com/kubernetes/test-infra/tree/master/prow)
to assign reviewers to the PR, set labels, run tests automatically, and so forth.

See [Reviewing and Merging Pull Requests](REVIEWING.md) for the PR review and
merge process used for Knative and for more information about [Prow](./REVIEWING.md#prow).

## Issues

GitHub issues can be used to report bugs or submit feature requests.

When reporting a bug please include the following key pieces of information:

*   The version of the project you were using (version number, git commit, etc)
*   Operating system you are using
*   The exact, minimal, steps needed to reproduce the issue. Submitting a 5 line
    script will get a much faster response from the team than one that's
    hundreds of lines long.

## Third-party code
* All third-party code must be placed in `vendor/` or `third_party/` folders.
* `vendor/` folder is managed by [dep](https://github.com/golang/dep) and stores
the source code of third-party Go dependencies. `vendor/` folder should not be 
modified manually.
* Other third-party code belongs in `third_party/` folder.
* Third-party code must include licenses.

A non-exclusive list of code that must be places in `vendor/` and `third_party/`:
* Open source, free software, or commercially-licensed code.
* Tools or libraries or protocols that are open source, free software, or commercially licensed.
* Derivative works of third-party code.
* Excerpts from third-party code.

### Adding a new third-party dependency to `third_party/` folder
* Create a sub-folder under `third_party/` for each component.
* In each sub-folder, make sure there is a file called LICENSE which contains the appropriate
 license text for the dependency. If one doesn't exist then create it. More details on this below.
* Check in a pristine copy of the code with LICENSE and METADATA files. 
 You do not have to include unused files, and you can move or rename files if necessary,
 but do not modify the contents of any files yet.
* Once the pristine copy is merged into master, you may modify the code.

### LICENSE
The license for the code must be in a file named LICENSE. If it was distributed like that,
you're good. If not, you need to make LICENSE be a file containing the full text of the license. 
If there's another file in the distribution with the license in it, rename it to LICENSE 
(e.g., rename a LICENSE.txt or COPYING file to LICENSE). If the license is only available in 
the comments or at a URL, extract and copy the text of the license into LICENSE.

You may optionally document the generation of the LICENSE file in the local_modifications 
field of the METADATA file.

If there are multiple licenses for the code, put the text of all the licenses into LICENSE 
along with separators and comments as to the applications.
