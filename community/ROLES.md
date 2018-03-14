# Elafros Community Roles

This document describes the set of roles individuals may have within the Elafros
community, the requirements of each role, and the privileges that each role
grants.

*   [Role Summary](#role-summary)
*   [Collaborator](#collaborator)
*   [Member](#member)
*   [Reviewer](#reviewer)
*   [Approver](#approver)
*   [Lead](#lead)
*   [Administrator](#administrator)
*   [Vendor](#vendor)

## Role Summary

Here is the set of roles we use within the Elafros community, the general
responsibilities expected by individuals in each role, the requirements
necessary to join or stay in a given role, and the concrete manifestation of the
role in terms of permissions and privileges.

<table>
  <thead>
    <tr>
    <th>Role</th>
    <th>Responsibilities</th>
    <th>Requirements</th>
    <th>Privileges</th>
    <th>Scope</th>
    </tr>
  </thead>

  <tr>
    <td>Collaborator</td>
    <td>Casual contributor to the project</td>
    <td>n/a</td>
    <td>
        <p>Can submit PRs</p>
        <p>Commenting permission on the Elafros Team drive</p>
    </td>
    <td>GitHub Organization</td>
  </tr>

  <tr>
    <td>Member</td>
    <td>Regular active contributor in the community</td>
    <td>
        <p>Sponsored by 2 reviewers</p>
        <p>Has made multiple contributions to the project</p>
    </td>
    <td>
        <p>Member of the GitHub Elafros org</p>
        <p>Member of the Elafros Slack workspace</p>
        <p>Edit permission on the Elafros Team drive</p>
    </td>
    <td>GitHub Organization</td>
  </tr>

  <tr>
    <td>Approver</td>
    <td>
        <p>Approve contributions from other members</p>
    </td>
    <td>Highly experienced and active reviewer and contributor to an area</td>
    <td>Entry in one or more CODEOWNERS files in GitHub, and write permissions
        on one or more repos allowing PRs to be merged.
    </td>
    <td>GitHub Directory</td>
  </tr>

  <tr>
    <td>Lead</td>
    <td>
        <p>Set priorities for a functional area and approve proposals</p>
        <p>Triage incoming issues, set milestones, repo labels</p>
        <p>Run their working group</p>
    </td>
    <td>Sponsored by the technical oversight committee as documented
        <a href="./WORKING-GROUP-PROCESSES.md">here</a>.
    </td>
    <td>Write permissions on one or more repos allowing issues to be manipulated.</td>
    <td>Working Group</td>
  </tr>

  <tr>
    <td>Administrator</td>
    <td>Manage & control permissions</td>
    <td>Sponsored by the technical oversight committee</td>
    <td>
        <p>Admin privileges on the GitHub Elafros org and all its repos</p>
        <p>Admin privileges on the Elafros Slack workspace</p>
        <p>Admin privileges on the Elafros Team Drive</p>
        <p>Admin privileges on the Google Search Console for elafros.dev</p>
        <p>Admin privilege to the Elafros email lists.</p>
    </td>
    <td>
        <p>GitHub Organization</p>
        <p>Team Drive</p>
        <p>Slack</p>
    </td>
  </tr>
</table>

## Collaborator

Individuals may be added as an outside collaborator (with READ access) to a repo
in the Elafros GitHub organization without becoming a member. This allows them
to be assigned issues and PRs until they become a member, but will not allow
tests to be run against their PRs automatically nor allow them to interact with
the PR bot.

### Requirements

*   Working on some contribution to the project that would benefit from the
    ability to have PRs or Issues to be assigned to the contributor

*   Sponsored by 1 member

## Member

Established community members are expected to demonstrate their adherence to the
principles in this document, familiarity with project organization, roles,
policies, procedures, conventions, etc., and technical and/or writing ability.

Members are continuously active contributors in the community. They can have
issues and PRs assigned to them, may participate in working group meetings, and
pre-submit tests are automatically run for their PRs. Members are expected to
remain active contributors to the community.

All members are encouraged to help with the code review burden, although each PR
must be reviewed by an official Approver.

When reviewing, should focus on code quality and correctness, including testing
and factoring.

May also review for more holistic issues, but not a requirement.

### Requirements

*   Has made multiple contributions to the project or community. Contributions
    may include, but are not limited to:

    *   Authoring or reviewing PRs on GitHub

    *   Filing or commenting on issues on GitHub

    *   Contributing to working group or community discussions

*   Subscribed to
    [elafros-dev@googlegroups.com](https://groups.google.com/forum/#!forum/elafros-dev)

*   Actively contributing to 1 or more areas.

*   Sponsored by 1 approver.

    *   Done by adding GitHub user to Elafros organization

### Responsibilities and privileges

*   Responsive to issues and PRs assigned to them

*   Active owner of code they have contributed (unless ownership is explicitly
    transferred)

    *   Code is well tested

    *   Tests consistently pass

    *   Addresses bugs or issues discovered after code is accepted

Members who frequently contribute code are expected to proactively perform code
reviews and work towards becoming an approver for the area that they are active
in.

## Approver

Code approvers are able to both review and approve code contributions. While
code review is focused on code quality and correctness, approval is focused on
holistic acceptance of a contribution including: backwards / forwards
compatibility, adhering to API and flag conventions, subtle performance and
correctness issues, interactions with other parts of the system, etc. Approver
status is scoped to a part of the codebase.

### Requirements

The following apply to the part of the codebase for which one would be an
approver in an OWNERS file:

*   Reviewer of the codebase for at least 3 months

*   Primary reviewer for at least 10 substantial PRs to the codebase

*   Reviewed or merged at least 30 PRs to the codebase

*   Nominated by an area lead

    *   With no objections from other leads

    *   Done through PR to update a CODEOWNERS file

### Responsibilities and privileges

The following apply to the part of the codebase for which one would be an
approver in an CODEOWNERS file:

*   Approver status may be a precondition to accepting large code contributions

*   Demonstrate sound technical judgement

*   Responsible for project quality control via [code
    reviews](https://github.com/elafros/elafros/blob/master/community/REVIEWING.md)

    *   Focus on holistic acceptance of contribution such as dependencies with
        other features, backwards / forwards compatibility, API and flag
        definitions, etc

*   Expected to be responsive to review requests as per [community
    expectations](https://github.com/elafros/elafros/blob/master/community/REVIEWING.md);

*   Mentor contributors and reviewers

*   May approve code contributions for acceptance

## Lead

Working group leads, or just ‘leads’, are approvers of an entire area that have
demonstrated good judgement and responsibility. Leads accept design proposals
and approve design decisions for their area of ownership.

### Requirements

Getting to be a lead of an existing working group:

*   Recognized as having expertise in the group’s subject matter.

*   Approver for some part of the codebase for at least 3 months

*   Member for at least 1 year (relevant as Elafros ages a bit more)

*   Primary reviewer for 20 substantial PRs

*   Reviewed or merged at least 50 PRs

*   Sponsored by the technical oversight committee

Establishing the leads for a new working group:

*   Originally authored or contributed major functionality to an area

*   An approver in the top-level OWNERS file for the group’s code

*   Approver for some part of the codebase for at least 3 months

*   Member for at least 1 year

*   Primary reviewer for 20 substantial PRs

*   Reviewed or merged at least 50 PRs

*   Sponsored by the technical oversight committee

### Responsibilities and privileges

The following apply to the area / component for which one would be an owner.

*   Run their working group as outlined in the [Working Group
    Processes](WORKING-GROUP-PROCESSES.md) document.

*   Design/proposal approval authority over the area / component, though
    escalation to the technical oversight committee is possible.

*   Perform issue triage on GitHub.

*   Apply/remove/create/delete GitHub labels and milestones

*   Write access to repo (assign issues/PRs, add/remove labels and milestones,
    edit issues and PRs, edit wiki, create/delete labels and milestones)

*   Capable of directly applying lgtm + approve labels for any PR

    *   Expected to respect OWNERS files approvals and use standard procedure
        for merging code

*   Expected to work to holistically maintain the health of the project through:

    *   Reviewing PRs

    *   Fixing bugs

    *   Mentoring and guiding approvers, reviewers, members, and contributors

## Administrator

Administrators are responsible for the bureaucratic aspects of the project.

### Requirements

*   Assigned by technical oversight committee.

### Responsibilities and privileges

*   Manage the Elafros GitHub repo, including granting membership and
    controlling repo read/write permissions

*   Manage the Elafros Slack workspace

*   Manage the Elafros Google group forums

*   Manage the Google Search Console settings for elafros.dev

## Vendor

Vendors contribute extensions to the Elafros project in the form of new
adapters, translated documentation, interesting examples, etc.

### Requirements

*   Sponsored by a member

### Responsibilities and privileges

*   Each vendor receives access to a subdirectory in the extensions repo, and
    each directory has got a distinct OWNERS file granting approver & reviewer
    permissions to the vendor.
