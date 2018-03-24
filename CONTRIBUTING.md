# Elafros Contribution and Project Roles

The Elafros project is open sourced under [the Apache 2 license](./LICENSE).
Its source code is freely available at
[github.com/elafros/elafros](https://github.com/elafros/elafros).

* [Community](#community)
* [Contributors](#contributors)
* [Maintainers](#maintainers)
* [Admins](#admins)

**Every community member, contributor, maintainer and admin is bound at all
times while representing Elafros to uphold the project's [Code of
Conduct](./code-of-conduct.md).**

## Community

The Elafros community is open. It is comprised of everyone who uses, wants
to use, or is just curious about the Elafros project. The members of the
Elafros community are the future contributors and maintainers of this project.

**Mailing list:** [elafros-users@googlegroups.com](https://groups.google.com/forum/#!forum/elafros-users)
(public, open to everyone)

**Slack channel:** [#community](https://elafros.slack.com#community) (free, but Slack account is required)

## Contributors

Elafros contributors are the individuals who have had an impact
on Elafros through their contributions to the project itself. While all
[have signed a CLA](#contributor-license-agreement), this isn’t an otherwise
formally defined group, which today maps somewhat to [GitHub
contributors](https://github.com/elafros/elafros/graphs/contributors).

Active contributors will be invited to join [the Elafros Slack team (a paid account)](https://elafros.slack.com).

**Mailing list:** [elafros-dev@googlegroups.com](https://groups.google.com/forum/#!forum/elafros-dev)
(public, anyone can use, frequently used)

### Contributor License Agreement

Contributions to this project must be accompanied by a Contributor License
Agreement. You (or your employer) retain the copyright to your contribution,
this simply gives us permission to use and redistribute your contributions as
part of the project. Head over to <https://cla.developers.google.com/> to see
your current agreements on file or to sign a new one.

You generally only need to submit a CLA once, so if you've already submitted one
(even if it was for a different project), you probably don't need to do it
again.

### Workflow

* All PRs must be pulled from [forks](./DEVELOPMENT.md#checkout-your-fork)
* All PRs must [be reviewed](#code-reviews)
* All PRs must [pass the tests](#prow)

#### Prow

The projects uses [prow](https://github.com/kubernetes/test-infra/tree/master/prow) to automatically run tests for every PR, and their failure blocks merging your changes.

If necessary, you can rerun the tests by simply adding the comment `/retest` to your PR.

prow has several other features that make PR management easier, like running the go linter or assigning labels.
A full list of commands understood by prow can be found in the [command help page](https://prow-internal.gcpnode.com/command-help?repo=elafros%2Felafros).

#### Code reviews

All submissions, including submissions by project members, require review. We
use [GitHub pull requests](https://help.github.com/articles/about-pull-requests/)
for this purpose.

## Maintainers

Elafros maintainers are the consensus-seeking deciders of what is and what is
not part of the Elafros project. This is a small group of active members of the
community who have a demonstrated history of leading Elafros projects and
making sound, appropriate project-level and technical decisions. Maintainers
alone have the right to merge changes into master, enforce [the Contributor
License Agreement](#contributor-license-agreement), ensure compliance with
all appropriate rules and regulations, and are ultimately responsible for
enforcing the Elafros project [Code of Conduct](./code-of-conduct.md).

All communication about Elafros design and development should be made public,
so this group should only talk privately about sensitive concerns (security
issues, CoC violations, etc).

**Current maintainers:**

* [vaikas@google.com](https://github.com/vaikas-google)
* [mattmoor@google.com](https://github.com/mattmoor)
* [argent@google.com ](https://github.com/evankanderson)

**Mailing list:** [elafros-maintainers@googlegroups.com](https://groups.google.com/forum/#!forum/elafros-maintainers)
(private, invite only, rarely used)

**Slack channel:** [#maintainers](https://elafros.slack.com#maintainers) (private)

## Admins

Elafros admins are the individuals in the project who are responsible for
billing and general project administration on GitHub, Slack, Google Groups,
etc. Elafros admins have NO special power or decision-making authority over
the code or project direction itself. It is purely an administrative, and
largely invisible role. Note, the Elafros admins group is intended to be as
small as possible while still provide sufficient redundancy in case of need
for immediate action (2-3 active users maximum).

**Current admins:**

* [dewitt@google.com](https://github.com/dewitt)
* [mchmarny@google.com](https://github.com/mchmarny)
* [isdal@google.com](https://github.com/isdal)

**Mailing list:** [elafros-admins@googlegroups.com](https://groups.google.com/forum/#!forum/elafros-admins)
(private, requires invitation)

**Slack channel:** [#admins](https://elafros.slack.com#admins) (private)
