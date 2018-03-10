# Contributing to Istio

So, you want to hack on Istio? Yay!

The following sections outline the process all changes to the Istio
repositories go through.  All changes, regardless of whether they are from
newcomers to the community or from the core team follow the
same process and are given the same level of review.

- [Working groups](#working-groups)
- [Code of conduct](#code-of-conduct)
- [Team values](#team-values)
- [Contributor license agreements](#contributor-license-agreements)
- [Design documents](#design-documents)
- [Contributing a feature](#contributing-a-feature)
- [Setting up to contribute to Istio](#setting-up-to-contribute-to-istio)
- [Pull requests](#pull-requests)
- [Issues](#issues)

## Working groups

The Istio community is organized into a set of [working groups](WORKING-GROUPS.md).
Any contribution to Istio should be started by first engaging with the appropriate working group.

## Code of conduct

All members of the Istio community must abide by the
[CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).
Only by respecting each other can we develop a productive, collaborative community.

## Team values

We promote and encourage a set of [shared values](VALUES.md) to improve our
productivity and inter-personal interactions.

## Contributor license agreements

We'd love to accept your patches! But before we can take them, you will have
to fill out the [Google CLA](https://cla.developers.google.com).

Once you are CLA'ed, we'll be able to accept your pull requests. This is
necessary because you own the copyright to your changes, even after your
contribution becomes part of this project. So this agreement simply gives us
permission to use and redistribute your contributions as part of the project.

## Design documents

Any substantial design deserves a design document. Design documents are written with Google Docs and
should be shared with the community by adding the doc to our [Team Drive](https://drive.google.com/corp/drive/u/0/folders/0AIS5p3eW9BCtUk9PVA)
and sending a note to the appropriate working group to let people know the doc is there. To get write access
to the drive, you'll need to be a [member](ROLES.md#member) of the Istio organization.

We have a common [design document template](https://docs.google.com/document/d/1cpolPNH_RtSobUjTkXyyxsCJT7EKD8fGiN2TgYLeJu8/edit#heading=h.7zgnj8bwqfld).
Just open the template and select "Make Copy" from the File menu in order to bootstrap your document.

Anybody can access the team drive for reading and commenting. If you are part of any of the Istio groups such
as [istio-dev@](https://groups.google.com/forum/#!forum/istio-dev), you automatically get permission to access the team drive.
If you're not part of one of these groups however, the first time you try to access the team drive you'll be asked to
request permission. This permission will always be granted and we do our best to grant access as fast as we can,
but there is a human involved there, so please forgive any delays.

## Contributing a feature

In order to contribute a feature to Istio you'll need to go through the following steps:

- Discuss your idea with the appropriate [working groups](WORKING-GROUPS.md) on the working 
group's mailing list.

- Once there is general agreement that the feature is useful, create a GitHub issue to track the discussion. The issue should include information
about the requirements and use cases that it is trying to address. Include a discussion of the proposed design and technical details of the
implementation in the issue.

- If the feature is substantial enough:

  - Working group leads will ask for a design document as outlined in the previous section.
  Create the design document and add a link to it in the GitHub issue. Don't forget to send a note to the 
  working group to let everyone know your document is ready for review.

  - Depending of the breath of the design and how contentious it is, the working group leads may decide
  the feature needs to be discussed in one or more working group meetings before being approved.

  - Once the major technical issues are resolved and agreed upon, post a note to the working group's mailing
  list with the design decision and the general execution plan.

- Submit PRs to istio/istio with your code changes.

- Submit PRs to istio/istio.github.io with documentation for your feature, including usage examples when possible.

> Note that we prefer bite-sized PRs instead of giant monster PRs. It's therefore preferable if you
can introduce large features in smaller reviewable changes that build on top of one another.

If you would like to skip the process of submitting an issue and
instead would prefer to just submit a pull request with your desired
code changes then that's fine. But keep in mind that there is no guarantee
of it being accepted and so it is usually best to get agreement on the
idea/design before time is spent coding it. However, sometimes seeing the
exact code change can help focus discussions, so the choice is up to you.

## Setting up to contribute to Istio

Check out this [README](https://github.com/istio/istio/blob/master/README.md) to learn about 
the Istio source base and setting up your [development environment](https://github.com/istio/istio/blob/master/DEV-GUIDE.md).

## Pull requests

If you're working on an existing issue, simply respond to the issue and express
interest in working on it. This helps other people know that the issue is
active, and hopefully prevents duplicated efforts.

To submit a proposed change:
- Fork the affected repository.
- Create a new branch for your changes.
- Develop the code/fix.
- Add new test cases. In the case of a bug fix, the tests should fail
  without your code changes. For new features try to cover as many
  variants as reasonably possible.
- Modify the documentation as necessary.
- Verify the entire CI process (building and testing) work.

While there may be exceptions, the general rule is that all PRs should
be 100% complete - meaning they should include all test cases and documentation
changes related to the change.

When ready, if you have not already done so, sign a
[contributor license agreements](#contributor-license-agreements) and submit
the PR.

See [Reviewing and Merging Pull Requests for Istio](REVIEWING.md) for the PR review and
merge process that is used by the maintainers of the project.

## Issues

GitHub issues can be used to report bugs or submit feature requests.

When reporting a bug please include the following key pieces of information:
- the version of the project you were using (e.g. version number,
  git commit, ...)
- operating system you are using
- the exact, minimal, steps needed to reproduce the issue.
  Submitting a 5 line script will get a much faster response from the team
  than one that's hundreds of lines long.
