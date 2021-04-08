# Duty

Every week we dedicate one individual (the community contact) to watch for user
issues and either answer them or redirect the questions to other who can. The
community contact's duty (subject to change) is as followed:

## Weekly check list

### Monday

- Join the `@serving-help` user group if you haven't been invited by the
  previous contact person using this
  [link](https://app.slack.com/client/T93ELUK42/browse-user-groups/user_groups/S0186KPJYG4)
- See [All days](#all-days)

### Friday

- Remove yourself from `@serving-help` usergroup and add the next contact using
  this
  [link](https://app.slack.com/client/T93ELUK42/browse-user-groups/user_groups/S0186KPJYG4).
  If you don't have permission, ask in the Slack channel
  `#steering-toc-questions`.
- Email the next contacts, cc'ing `knative-dev@` with a short summaries of the
  user questions encountered and links to them.
- Send updates to this process.
- See [All days](#all-days)

### All days

- Check the [Serving test grid](https://testgrid.knative.dev/serving) for
  flakiness to pick a test and focus on fixing it during your week. Once you
  pick the test flake, assign the corresponding bug filed by flakiness test
  reporter to yourself so that others won't pick the same test to fix.
- Check the
  [knative-users@](https://groups.google.com/forum/#!forum/knative-users)
  mailing list.
- Add your self as a member of Slack user group `@serving-help`
- Check Slack channel
  [#serving-questions](https://knative.slack.com/archives/C0186KU7STW) for
  unanswered questions. Any questions that relates to usability please instruct
  user to
  [open an usablity issue](https://github.com/knative/ux/issues/new?assignees=&labels=kind%2Ffriction-point&template=friction-point-template.md&title=)
  and to join the channel
  [#user-experience](https://knative.slack.com/archives/C01JBD1LSF3) to capture
  user feedback.
- [Triage issues in the serving repo](./TRIAGE.md). Quick links:
  - [Untriaged issues](https://github.com/knative/serving/issues?q=is%3Aissue+is%3Aopen+-label%3Atriage%2Faccepted+-label%3Atriage%2Fneeds-user-input)
  - [User feedback issues updated more than 3 days ago](https://github.com/knative/serving/issues?q=is%3Aissue+is%3Aopen+label%3Atriage%2Fneeds-user-input+updated%3A%3C%3D2021-03-13)
    -- **YOU NEED TO UPDATE THE DATE IN THE QUERY**
- Check Docs
  [unassigned issues / untriaged issues](https://github.com/knative/docs/issues?q=is%3Aopen+is%3Aissue+label%3Akind%2Fserving+label%3Atriage%2Fneeds-eng-input)
  for unanswered questions.
- Answer relevant
  [Stack Overflow Questions](https://stackoverflow.com/questions/tagged/knative-serving?tab=Newest)

## SLO

Participation is voluntary and based on good faith. We are only expected to
participate during our local office hour.

# Roster

We seed this rotation with all approvers from all the Serving workgroups,
excluding productivity. If you are no longer active in Knative, or if you are
contributing on personal capacity and do not have time to contribute in the
rotation, feel free to send a PR to remove yourself.

- [dprotaso](https://github.com/dprotaso)
- [julz](https://github.com/julz)
- [markusthoemmes](https://github.com/markusthoemmes)
- [nak3](https://github.com/nak3)
- [tcnghia](https://github.com/tcnghia)
- [vagababov](https://github.com/vagababov)
- [yanweiguo](https://github.com/yanweiguo)
- [ZhiminXiang](https://github.com/ZhiminXiang)

# Schedule

See [a machine-readable schedule here](schedule.rotation). The format is:

```
# comment lines are okay
#@ metadata: value of the metadata
RFC3339-date  |  username
```

You can see the current oncall at https://knative.party/ (which reads the
machine-readable file).
