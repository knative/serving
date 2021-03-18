# Triage process

This is heavily inspired by [this blog post](https://apenwarr.ca/log/20171213),
but that's a long read, so here's the short version.

The goal of triage is to make sure that the following are handled quickly:

- New bugs or issues, some of which might be high priority
- Consumer (developer or administrator) updates to an issue which might not be
  on the immediate radar of a contributor

The goal is for the project to be responsive to outside requests, while
shielding most of the team from the interrupt-y work of watching the triage
queue. An additional goal is to protect and respect the time of the triager /
support user.

For triage, the goal is to handle _all_ the bugs on the following lists
(searches):

- [Untriaged issues](https://github.com/knative/serving/issues?q=is%3Aissue+is%3Aopen+-label%3Atriage%2Faccepted+-label%3Atriage%2Fneeds-user-input)
- [User feedback issues updated more than 3 days ago](https://github.com/knative/serving/issues?q=is%3Aissue+is%3Aopen+label%3Atriage%2Fneeds-user-input+updated%3A%3C%3D2021-03-13)
  -- **YOU NEED TO UPDATE THE DATE IN THE QUERY**

When doing triage, the goal is to quickly process issues, getting them into the
right "bin". For small issues and user questions, it's reasonable to reply in
the issue with documentation or a reference to another issue, but you should
avoid _fixing_ anything when triaging -- if you're making PRs during triage,
you're doing it wrong.

At the end of reading an issue, you should do one of the following:

1. If the issue is clear and applies to serving, ensure it has the correct
   `/kind` and `/area` assigned. If you know enough, feel free to `/assign` to
   an individual, but area assignments should be sufficient for triage. Also
   consider if it is a `/good-first-issue`. After this, also mark the issue as
   `/triage accepted`, so it drops out of the list above.

1. Move it to the correct repo (for example, Istio-specific questions should
   probably go to `net-istio`). In some cases, you may need to create a new
   issue in the new repo and link / copy the current issue. If you do this, be
   sure to `@mention` the requestor and others on the old issue, and then
   `/close` the issue is serving with a link to the other repo.

1. If it's not clear what the problem or issue is, add a note for the requestor
   (or occasionally some other user on the thread), make sure that there is a
   reasonable `/kind` on the issue and mark it as `/triage needs-user-input` so
   that it's off the list for a few days. If a `needs-user-input` issue persists
   for longer than a week or so (past a second followup), it's reasonable to
   `/close` the issue and encourage the requester to reopen if they have more
   detail.

1. If the request can be resolved with documentation, is infeasable, or
   complete, follow up directly in the issue with the information, and `/close`
   the issue.
