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
controller has seen the latest updates to the spec). Example user and
system error scenarios are included below along with how the status is
presented to CLI and UI tools via the API.

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
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
```


## Build failed

If the Build steps failed while creating a Revision, you can examine
the `Failed` condition on the Build or the `BuildFailed` condition on
the Revision (which copies the value from the build referenced by
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
    # reason is a short status, message provides error details
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
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
  - type: BuildFailed
    status: True
    reason: BuildStepFailed
    # reason is a short status, message provides error details
    message: "Step XYZ failed with error message: $LASTLOGLINE"
```


## Revision not found by Route

If a Revision is referenced in the Route's `spec.rollout.traffic`, the
corresponding entry in the `status.traffic` list will be set to "Not
found", and the `TrafficDropped` condition will be marked as True,
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
  - type: RolloutInProgress
    status: False
  - type: TrafficDropped
    status: True
    reason: RevisionMissing
    # reason is a short status, message provides error details
    message: "Revision 'qyzz' referenced in rollout.traffic not found"
```


## Configuration not found by Route

If a Route references the `latestReadyRevisionName` of a Configuration
and the Configuration cannot be found, the corresponding entry in
`status.traffic` list will be set to "Not found", and the
`TrafficDropped` condition will be marked as True with a reason of
`ConfigurationMissing`.

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
  - type: RolloutInProgress
    status: False
  - type: TrafficDropped
    status: True
    reason: ConfigurationMissing
    # reason is a short status, message provides error details
    message: "Revision 'my-service' referenced in rollout.traffic not found"
```


## Latest Revision of a Configuration deleted

If the most recent (or most recently ready) Revision is deleted, the
Configuration will clear the `latestReadyRevisionName`. If the
Configuration is referenced by a Route, the Route will set the
`TrafficDropped` condition with reason `RevisionMissing`, as above.

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
```


## Deployment progressing slowly/stuck

See
[the kubernetes documentation for how this is handled for Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#failed-deployment). For
Revisions, we will start by assuming a single timeout for deployment
(rather than configurable), and report that the Revision was not
Ready, with a reason `ProgressDeadlineExceeded`. Note that we will
only report `ProgressDeadlineExceeded` if we could not determine
another reason (such as quota failures, missing build, or container
execution failures).

Kubernetes controllers will continue attempting to make progress
(possibly at a less-aggressive rate) when they encounter a case where
the desired status cannot match the actual status, so if the
underlying deployment is slow, it might eventually finish after
reporting `ProgressDeadlineExceeded`.

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
    message: "Unable to create pods for more than 120 seconds."
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
  - type: RolloutInProgress
    status: True
    reason: ProgressDeadlineExceeded
    # reason is a short status, message provides error details
    message: "Unable to update traffic split for more than 120 seconds."
```


## Container image not present in repository

Revisions might be created while a Build is still creating the
container image or uploading it to the repository. If the build is
being performed by a CRD in the cluster, the spec.buildName attribute
will be set (and see the [Build failed](#build-failed) example). In
other cases when the build is not supplied, the container image
referenced might not be present in the registry (either because of a
typo or because it was deleted). In this case, the Ready condition
will be set to False with a reason of ContainerMissing. This condition
could be corrected if the image becomes available at a later time. We
can also make a defensive copy of the container image to avoid this
error due to deleted source container.

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
  - type: Failed
    status: True
    reason: ContainerMissing
    message: "Unable to fetch image 'gcr.io/...': <literal error>"
```


## Container image fails at startup on Revision

Particularly for development cases with interpreted languages like
Node or Python, syntax errors or the like might only be caught at
container startup time. For this reason, implementations may choose to
start a single copy of the container on deployment, before making the
container Ready. If the initial container fails to start, the `Ready`
condition will be set to False and the reason will be set to
`ExitCode:%d` with the exit code of the application, and the last line
of output in the message. Additionally, the Revision will include a
`logsUrl` which provides the address of an endpoint which can be used to
fetch the logs for the failed process.

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
    reason: ExitCode:127
    message: "Container failed with: SyntaxError: Unexpected identifier"
```
