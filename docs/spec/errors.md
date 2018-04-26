# Error Conditions and Reporting

Elafros uses the standard Kubernetes API pattern for reporting
configuration errors and current state of the system by writing the
report in the `status` section. There are two mechanisms commonly used
in status:

* conditions represent true/false statements about the current state
  of the resource.

* other fields may provide status on the most recently retrieved state
  of the system as it relates to the resource (example: number of
  replicas or traffic assignments).

Both of these mechanisms often include additional data from the
controller such as `observedGeneration` (to determine whether the
controller has seen the latest updates to the spec).

Conditions provide an easy mechanism for client user interfaces to
indicate the current state of resources to a user. Elafros resources
should follow these patterns:

1. Each resource should define a small number of success conditions as
   Types. This should bias towards fewer than 5 high-level progress
   categories which are separate and meaningful for customers. For a
   Revision, these might be `BuildSucceeded`, `ResourcesAvailable` and
   `ContainerHealthy`.
2. Where it makes sense, resources should define a top-level "happy
   state" condition type which indicates that the resource is set up
   correctly and ready to serve. For long-running resources, this
   should be named `Ready`. For objects which run to completion, the
   object should be named `Succeeded`.
3. Each condition's status should be one of:
   * `Unknown` when the controller is actively working to achieve the
     condition.
   * `False` when the reconciliation has failed. This should be a terminal
     failure state until user action occurs.
   * `True ` when the reconciliation has succeeded. Once all transition
     conditions have succeeded, the "happy state" condition should be set
     to `True`.
     
   Type names should be chosen such that these interpretations are clear:
   
   > `BuildSucceeded` works because `True` = success and `False` = failure.
   
   > `BuildCompleted` does not, because `False` could mean "in-progress".
   
   Conditions may also be omitted entirely if reconciliation has been
   skipped. When all conditions have succeeded, the "happy state"
   should clear other conditions for output legibility. Until the
   "happy state" is set, conditions should be persisted for the
   benefit of UI tools representing progress on the outcome.
   
4. Conditions with a status of `False` will also supply additional details
   about the failure in the "Reason" and "Message" sections -- both of
   these should be considered to have unlimited cardinality, unlike
   Type. If a resource has a "happy state" type, it will surface the
   `Reason` and `Message` from the first failing sub Condition.

Example user and system error scenarios are included below along with
how the status is presented to CLI and UI tools via the API.

* [Revision failed to become Ready](#revision-failed-to-become-ready)
* [Build failed](#build-failed)
* [Revision not found by Route](#revision-not-found-by-route)
* [Configuration not found by Route](#configuration-not-found-by-route)
* [Latest Revision of a Configuration deleted](#latest-revision-of-a-configuration-deleted)
* [Resource exhausted while creating a revision](#resource-exhausted-while-creating-a-revision)
* [Deployment progressing slowly/stuck](#deployment-progressing-slowly-stuck)
* [Traffic shift progressing slowly/stuck](#traffic-shift-progressing-slowly-stuck)
* [Container image not present in repository](#container-image-not-present-in-repository)
* [Container image fails at startup on Revision](#container-image-fails-at-startup-on-revision)


## Revision failed to become Ready

If the latest Revision fails to become `Ready` for any reason within some reasonable
timeframe, the Configuration should signal this
with the `LatestRevisionReady` status, copying the reason and the message
from the `Ready` condition on the Revision.

```yaml
...
status:
  latestReadyRevisionName: abc
  latestCreatedRevisionName: bcd  # Hasn't become "Ready"
  conditions:
  - type: LatestRevisionReady
    status: False
    reason: ContainerHealthy
    message: "Unable to start because container is missing and build failed."
```


## Build failed

If the Build steps failed while creating a Revision, you can examine
the `Failed` condition on the Build or the `BuildSucceeded` condition
on the Revision (which copies the value from the build referenced by
`spec.buildName`). In addition, the Build resource (but not the
Revision) should have a status field to link to the log output of the
build.

```http
GET /apis/build.dev/v1alpha1/namespaces/default/builds/build-1acub3
```
```yaml
...
status:
  # Link to log stream; could be ELK or Stackdriver, for example
  buildLogsLink: "http://logging.infra.mycompany.com/...?filter=..."
  conditions:
  - type: Failed
    status: True
    reason: BuildStepFailed  # could also be SourceMissing, etc
    message: "Step XYZ failed with error message: $LASTLOGLINE"
```


```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/revisions/abc
```
```yaml
...
status:
  conditions:
  - type: Ready
    status: False
    reason: BuildFailed
    message: "Build Step XYZ failed with error message: $LASTLOGLINE"
  - type: BuildSucceeded
    status: False
    reason: BuildStepFailed
    message: "Step XYZ failed with error message: $LASTLOGLINE"
```


## Revision not found by Route

If a Revision is referenced in the Route's `spec.traffic`, the
corresponding entry in the `status.traffic` list will be set to "Not
found", and the `AllTrafficAssigned` condition will be marked as False
with a reason of `RevisionMissing`.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/routes/abc
```
```yaml
...
status:
  traffic:
  - revisionName: abc
    name: current
    percent: 100
  - revisionName: "Not found"
    name: next
    percent: 0
  conditions:
  - type: Ready
    status: False
    reason: RevisionMissing
    message: "Revision 'qyzz' referenced in traffic not found"
  - type: AllTrafficAssigned
    status: False
    reason: RevisionMissing
    message: "Revision 'qyzz' referenced in traffic not found"
```


## Configuration not found by Route

If a Route references the `latestReadyRevisionName` of a Configuration
and the Configuration cannot be found, the corresponding entry in
`status.traffic` list will be set to "Not found", and the
`AllTrafficAssigned` condition will be marked as False with a reason
of `ConfigurationMissing`.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/routes/abc
```
```yaml
...
status:
  traffic:
  - revisionName: "Not found"
    percent: 100
  conditions:
  - type: Ready
    status: False
    reason: ConfigurationMissing
    message: "Revision 'my-service' referenced in rollout.traffic not found"
  - type: AllTrafficAssigned
    status: False
    reason: ConfigurationMissing
    message: "Revision 'my-service' referenced in rollout.traffic not found"
```


## Latest Revision of a Configuration deleted

If the most recent Revision is deleted, the Configuration will set
`LatestRevisionReady` to False.

If the deleted Revision was also the most recent to become ready, the
Configuration will also clear the `latestReadyRevisionName`. Additionally,
if the Configuration in this case is referenced by a Route, the Route will
set the `AllTrafficAssigned` condition to False with reason
`RevisionMissing`, as above.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/configurations/my-service
```
```yaml
...
metadata:
  generation: 1234  # only updated when spec changes
  ...
spec:
  ...
status:
  latestCreatedRevision: abc
  conditions:
  - type: LatestRevisionReady
    status: False
    reason: RevisionMissing
    message: "The latest Revision appears to have been deleted."
  observedGeneration: 1234
```


## Resource exhausted while creating a revision

Since a Revision is only metadata, the Revision will be created, but
will have a condition indicating the underlying failure, possibly
indicating the failed underlying resource. In a multitenant
environment, the customer might not have have access or visibility
into the underlying resources in the hosting environment.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/revisions/abc
```
```yaml
...
status:
  conditions:
  - type: Ready
    status: False
    reason: NoDeployment
    message: "The controller could not create a deployment named ela-abc-e13ac."
  - type: ResourcesProvisioned
    status: False
    reason: NoDeployment
    message: "The controller could not create a deployment named ela-abc-e13ac."
```


## Deployment progressing slowly/stuck

See
[the kubernetes documentation for how this is handled for Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#failed-deployment). For
Revisions, we will start by assuming a single timeout for deployment
(rather than configurable), and report that the Revision was not Ready,
with a reason `ProgressDeadlineExceeded`. Note that we will only report
`ProgressDeadlineExceeded` if we could not determine another reason (such
as quota failures, missing build, or container execution failures).

Since container setup time also affects the ability of 0 to 1
autoscaling, the `Ready` failure with `ProgressDeadlineExceeded`
reason should be considered a terminal condition, even if Kubernetes
might attempt to make progress even after the deadline.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/revisions/abc
```
```yaml
...
status:
  conditions:
  - type: Ready
    status: False
    reason: ProgressDeadlineExceeded
    message: "Did not pass readiness checks in 120 seconds."
```


## Traffic shift progressing slowly/stuck

Similar to deployment slowness, if the transfer of traffic (either via
gradual or abrupt rollout) takes longer than a certain timeout to
complete/update, the `RolloutInProgress` condition will remain at
True, but the reason will be set to `ProgressDeadlineExceeded`.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/routes/abc
```
```yaml
...
status:
  traffic:
  - revisionName: abc
    percent: 75
  - revisionName: def
    percent: 25
  conditions:
  - type: Ready
    status: False
    reason: ProgressDeadlineExceeded
    # reason is a short status, message provides error details
    message: "Unable to update traffic split for more than 120 seconds."
```


## Container image not present in repository

Revisions might be created while a Build is still creating the
container image or uploading it to the repository. If the build is
being performed by a CRD in the cluster, the `spec.buildName`
attribute will be set (and see the [Build failed](#build-failed)
example). In other cases when the build is not supplied, the container
image referenced might not be present in the registry (either because
of a typo or because it was deleted). In this case, the `Ready`
condition will be set to `False` with a reason of
`ContainerMissing`. This condition could be corrected if the image
becomes available at a later time. Elafros could also make a defensive
copy of the container image to avoid having to surface this error if
the original docker image is deleted.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/revisions/abc
```
```yaml
...
status:
  conditions:
  - type: Ready
    status: False
    reason: ContainerMissing
    message: "Unable to fetch image 'gcr.io/...': <literal error>"
  - type: ContainerHealthy
    status: False
    reason: ContainerMissing
    message: "Unable to fetch image 'gcr.io/...': <literal error>"
```


## Container image fails at startup on Revision

Particularly for development cases with interpreted languages like
Node or Python, syntax errors might only be caught at container
startup time. For this reason, implementations should start a copy of
the container on deployment, before marking the container `Ready`. If
this container fails to start, the `Ready` condition will be set to
`False`, the reason will be set to `ExitCode%d` with the exit code of
the application, and the termination message from the container will
be provided. (Containers will be run with the default
`terminationMessagePath` and a `terminationMessagePolicy` of
`FallbackToLogsOnError`.) Additionally, the Revision `status.logsUrl`
should be present, which provides the address of an endpoint which can
be used to fetch the logs for the failed process.

```http
GET /apis/elafros.dev/v1alpha1/namespaces/default/revisions/abc
```
```yaml
...
status:
  logUrl: "http://logging.infra.mycompany.com/...?filter=revision=abc&..."
  conditions:
  - type: Ready
    status: False
    reason: ExitCode127
    message: "Container failed with: SyntaxError: Unexpected identifier"
  - type: ContainerHealthy
    status: False
    reason: ExitCode127
    message: "Container failed with: SyntaxError: Unexpected identifier"
```
