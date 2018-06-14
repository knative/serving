# Knative Working Group Processes

This document describes the processes we use to manage the Knative working
groups. This includes how they are formed, how leads are established, how they
are run, etc.

*   [Why working groups?](#why-working-groups)
*   [Proposing a new working group](#proposing-a-new-working-group)
*   [Setting up a working group](#setting-up-a-working-group)
    *   [Dissolving a working group](#dissolving-a-working-group)
*   [Running a working group](#running-a-working-group)
    *   [Be open](#be-open)
    *   [Making decisions](#making-decisions)
    *   [Subgroups](#subgroups)
    *   [Escalations](#escalations)

## Why working groups?

Knative working groups are organizations responsible for the design and
implementation of large architectural aspects of the overall Knative project.
Working groups operate with a fair amount of autonomy within the broader scope
of the project. They tend to be long-lived, representing major initiatives over
Knative’s lifetime.

Some working groups focus on specific technologies. Other working groups are
cross-cutting in nature.

The technical oversight committee is responsible for the Knative project as a
whole. It sets the overall direction of the project, helps make crosscutting
architectural decisions, helps establish and dissolve working groups, and helps
ensure all working groups are generally rowing in the same direction

Although working groups are relatively lightweight structures, we want to keep
the number of working groups low in order to keep things manageable.

## Proposing a new working group

If you’ve identified a substantial architectural area which would benefit from
long-lived, concerted and focused design, then you should consider creating a
new working group. To do so, you need to:

*   **Create a charter**. This should be a few paragraphs explaining:

    *   The mission of the working group

    *   The goals of the working group (problems being solved)

    *   The scope of the working group (topics, subsystems, code repos, areas of
        responsibility)

*   **Nominate an initial set of leads**. The leads set the agenda for the
    working group and serve as final arbiters on any technical decision. See
    [below](#leads) for information on the responsibilities of leads and
    requirements for nominating them.

*   **Prepare a Roadmap**. Create a preliminary 3 month roadmap for what the
    working group would focus on.

*   **Send an Email**. Write up an email with your charter, nominated leads, and
    roadmap, and send it to
    [knative-tech-oversight@](mailto:knative-tech-oversight@googlegroups.com).
    The technical oversight committee will evaluate the request and decide
    whether the working group should be formed, whether it should be merely a
    subgroup of an existing working group, or whether it should be subsumed by
    an existing working group.

## Setting up a working group

Once approval has been granted by the technical oversight committee to form a
working group, the working group leads need to take a few steps to establish the
working group:

*   **Create a Google Drive Folder**. Create a folder to hold your
    working group documents within this parent
    [folder](https://drive.google.com/corp/drive/folders/0APnJ_hRs30R2Uk9PVA). Call
    your folder "GROUP_NAME".

*   **Create a Meeting Notes Document**. Create a blank document in the above
    folder and call it "GROUP_NAME Group Meeting Notes".

*   **Create a Roadmap Document**. Create a document in the above folder and
    call it "GROUP_NAME Group Roadmap". Put your initial roadmap in the
    document.

*   **Create a Wiki**. Create a wiki page on
    [GitHub](https://github.com/knative/serving) titled "GROUP_NAME Design
    Decisions". This page will be used to track important design decisions made
    by the working group.

*   **Create a Public Google Group**. Call the group "knative-*group_name*" (all
    in lowercase, dashes for spaces). This mailing list must be open to all.

*   **Schedule a Recurring Meeting**. Create a recurring meeting (weekly or
    bi-weekly, 30 or 60 minutes) and call the meeting GROUP_NAME Group Sync-Up".
    Attach the meeting notes document to the calendar event. Generally schedule
    these meetings between 9:00AM to 2:59PM Pacific Time. Invite the public
    Google group to the meeting.

*   **Register the Working Group**. Go to
    [WORKING-GROUPS.md](https://github.com/knative/serving/blob/master/community/WORKING-GROUPS.md)
    and add your working group name, the names of the leads, the working group
    charter, and a link to the meeting you created.

*   **Announce your Working Group**. Send a note to
    [knative-dev@](mailto:knative-dev@googlegroups.com) and
    [knative-tech-oversight@](mailto:knative-tech-oversight@googlegroups.com) to
    announce your new working group. Include your charter in the email and
    provide links to the meeting invitation.

Congratulations, you now have a fully formed working group!

### Dissolving a working group

Some working groups are ephemeral or naturally reach the end of their useful
life. Working group leads can petition to dissolve their working groups by
emailing
[knative-tech-oversight@googlegroups.com](mailto:knative-tech-oversight@googlegroups.com).
The technical oversight committee takes ownership of any artifacts created or
owned by the dissolved working group. The technical oversight committee also
reserves the right to dissolve or recharter working groups over time as
necessary, though they will strive to first discuss this in committee meetings
and open community discussion.

## Leads

Each working group should have at least 2 leads, ideally 3, though young working
groups may have only 1 lead initially. Working groups should strive to include
representatives from multiple organizations as both leads and members. Working
group leads must be Members of the Knative project (that is, have made multiple
contributions to the project in the form of code, design, or documentation).

Please see the [Community Roles](ROLES.md) document for a description of a
lead’s role and requirements.

## Running a working group

Leads are responsible for running a working group. Running the group involves a
few activities:

*   **Meetings**. Prepare the agenda and run the regular working group meetings.
    Ensure the meetings are recorded, and properly archived.

*   **Notes**. Ensure that meeting notes are kept up to date. Provide a link to
    the recorded meeting in the notes. The lead may delegate note-taking duties.

*   **Wiki**. Ensure that significant design decisions are captured in the Wiki.
    In the Wiki, include links to useful design documents, any interesting
    GitHub issues or PRs, posts to the mailing lists, etc. The wiki should
    provide a good feel for where the mind of the working group is at and where
    things are headed.

*   **Roadmap**. Establish **and maintain** a roadmap for the working group
    outlining the areas of focus for the working group over the next 3 months.

*   **Report**. Report current status to the main community meeting every 6
    weeks.

### Be open

The community design process is done in the open. Working groups should
communicate primarily through the public working group meetings, through design
documents in the working group’s folder, through GitHub issues, and GitHub PRs.
Avoid private emails and/or meeting when possible.

### Making decisions

In general, working groups operate in a highly cooperative environment. Working
groups discuss designs in the open and take input from the community at large
when making technical choices. The working group leads are ultimately
responsible for setting the direction of the working group and making the tough
technical choices affecting the working group.

### Subgroups

Subgroups are ad hoc subteams within a working group with a special focus on a
set of problems or technologies. We don’t formalize processes for subgroups,
each working group can decide when subgroups are needed and how they operate.

### Escalations

Working groups can get blocked on specific technical disagreements. Leads are
expected to generally resolve such issues and allow work to progress.

Sometimes, different working groups can have conflicting goals or requirements.
Leads from all affected working groups generally work together and come to an
agreeable conclusion.

In all cases, remaining blocking issues can be raised to the [technical
oversight committee](TECH-OVERSIGHT-COMMITTEE.md) to help resolve the situation.
To trigger an escalation, create an issue in the `knative/serving` repo and
assign it to the **@knative/tech-oversight-committee** team.
