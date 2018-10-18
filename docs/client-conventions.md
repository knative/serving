# Client Conventions

This document describes conventions that Knative domain-specific clients can
follow to achieve specific end-user goals. It is intended as a set of best
practices for client implementors, and also as advice to direct users of the API
(for example, with kubectl).

These conventions are merely conventions:
 * They are optional; you can use Knative entirely validly without them.
 * They are designed to be useful even when some clients are not obeying the
   conventions. Each convention describes what happens in the presence of
   convention-unaware clients.

Some of the conventions involve the client setting labels or annotations; the
`client.knative.dev/*` label/annotation namespace is reserved for documented
Knative client convetions.

## Determine when an action is complete

As Knative is (like all of Kubernetes) a declarative API, the user expresses
their desire by changing some values in the Knative objects. Clients need not be
declarative, and might have expressions of user intent like "Deploy this code"
or "Change these environment variables". To tell when such an action is
complete, the client can look at the status conditions.

Each Knative object has a `Ready` status condition. When a change is initiated,
the controller flips this to `Unknown`. When the serving state again reflects
exactly what the spec of the object specifies, the `Ready` condition will flip
to `True`; this indicates the operation was a success. If reflecting the spec in
the serving state is impossible, the `Ready` condition will flip to `False`;
this indicates the operation was a failure, and the message of the status
condition should indicate something in English about why (and the Reason field
can indicate an emumeration suitable for i18n). Either `True` or `False`
indicates the operation is complete, for better or worse.

Note that someone else could start another operation while the cient was waiting
for its operation. A conventional client still waits for the `Ready` condition
to land at `True` or `False`, and then describes to the user what happened using
logic based on the intended effect.

For example:
 * Client A deploys image `gcr.io/foods/vegetables:eggplant`
 * While that is not yet Ready, client B deploys `gcr.io/foods/vegetables:squash`
 * The `eggplant` revision becomes Ready: True, and the service moves traffic to it.
 * The `squash` revision fails to bind to a port, and becomes Ready: False
 * The Service switches from Ready: Unknown to Ready: False because `squash` failed.

Both client A and B should wait for the last step in this procedure.
 * Client A sees that latestReadyRevision is the revision with the
   [nonce](#associate-modifications-with-revisions) it specified, and that
   `latestCreatedRevision` is not. It tells the user that deploying was
   successful.
 * Client B sees that `latestCreatedRevision` is the revision with the nonce it
   specified; it reports the failure with the appropriate message.

The rule is "Wait for `Ready` to become `True` or `False`, then report on
whether your intent was accomplished". The `Ready` success or failure can be
part of this report, but may be confusing (as in the example) if it's the only
thing you report.

## Associate modifications with Revisions

Every time the client changes a Service or Configuration in a way that results
in a new Revision, it should change the label `client.knative.dev/nonce` to a
new random value, chosen from a wide enough space to avoid collisions.

This way, the client can make a label selector query for Revisions with this
nonce value to find the Revision the particular change generated. The client can
use that revision to, for example, inform the user about the readiness of their
requested change.

### In the presence of non-conventional clients

If an unconventional client neglects to reset this value, the label selector
query could result in more than one revision; the client should pick the one
with the lowest `configurationGeneration` in this case.

## Force creation of a new Revision

The way to deploy new code with a previously-used tag is to make a new Revision,
which the Revision controller will re-pull and lock it to the current image at
that tag. Unfortunately Knative won't create a new Revision if you don't change
the Configuration.

The same `client.knative.dev/nonce` annotation can help in forcing the creation
of a new Revision; if the nonce is changed, the Configuration controller must
make one even if nothing else has changed.

## Change non-code attributes

When the user specifies they'd like to change an environment variable (or a
memory allocation, or a concurrency setting...), and does not specify that
they'd like to deploy a change in code, the user would be quite surprized to
find the newest image at their deployed tag running in the cloud.

### General idea

Since the Revision controller will resolve an image tag for every Revision
creation, we need a way to express a non-code change. Clients should do this by
changing the `image` field to be a digest-based image URL supplied by the
Revision `status.imageDigest` field, while marking the original tag-based user
intent in an annotation.

### Procedure

 1. Get the current state of the Service in question.
 2. Get a **base revision**, the Revision corresponding to the fetched state of
    the Service: Issue a label selector query for revisions with the
    `client.knative.dev/nonce` label listed in the
    `revisionTemplate.metadata`. If this yields exactly one Revision (after
    waiting a sensible timeout for a revision to appear with that nonce), that
    is the base revision. If not, fetch the `latestCreatedRevision` from the
    status, and uses that as the base revision.
 3. Copy the `status.imageDigest` field from the base revison into the `image`
    field of the Service. This ensures the running code stays the same.
 4. Make whatever other modifications to the Service.
 5. Add the `client.knative.dev/user-image` annotation to the Service,
    containing the original tag-based URL of the image.
 6. Add the nonce label, as usual.
 7. Post the resulting Service to create a new Revision.

### Display images

Since we're now filling in the `image` field with a URL the user may never have
specified by hand, a client can display the image for human-readability as the
contents of the `client.knative.dev/user-image` annotation, combined with the
note that it is "at digest <digest>", fetched from the `imageDigest` of the
revision (or the `image` field iteself of the Service).
