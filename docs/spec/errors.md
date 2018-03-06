# Error Conditions and Reporting

The standard kubernetes API pattern for reporting configuration errors
and current state of the system is to use the status section to report
the state of the system observed by the controller. There are two
mechanisms commonly used in status:

* conditions represent true/false statements about the current state
  of the resource.

* other fields may provide status on the most-recently retrieved state
  of the world as it relates to the resource (example: number of
  replicas or traffic assignments).

Both of these mechanisms will often include additional data from the
controller such as `observedGeneration` (to determine whether the
controller has seen the latest updates to the spec). Below are some
example error scenarios (either due to user or system error), along
with how the status would be presented to CLI and UI tools via the
API:

## Revision failed to become Ready

If the latest Revision fails to become "Ready" within some reasonable
timeframe (for whatever reason), the Configuration should signal this
with the `LatestRevisionReady` status, copying the reason and message
from the Ready condition on the Revision.

```yaml
...
status:
  latestReadyRevisionName: abc
  latestCreatedRevisionName: bcd  # Hasn't become became "Ready"
  conditions:
  - type: LatestRevisionReady
    status: False
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
```


## Build failed

If the Build steps failed while creating a Revision, this would be
detectable by examining the Failed condition on the Build or the
BuildFailed condition on the Revision (which copies the value from the
build referenced by `spec.buildName`). In addition, the Build resource
(but not the Revision) should have a status field to link to the log
output of the build.

```http
GET /apis/build.dev/v1alpha1/namespaces/default/builds/build-1acub3
```
```yaml
...
status:
  # Link to log stream; could be ELK or Stackdriver, for example
  buildLogsLink: "http://logging.infra.mycompany.com/...?filter=..."
  conditions:
  - type: Complete
    status: True
  - type: BuildFailed
    status: True
    reason: BuildStepFailed  # could also be SourceMissing, etc
    # reason is for machine consumption, message is for human consumption
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
    # reason is for machine consumption, message is for human consumption
    message: "Step XYZ failed with error message: $LASTLOGLINE"
```


## Revision not found by Route

If a Revision is referenced in the Route's `spec.rollout.traffic`, the
corresponding entry in the `status.traffic` list will be set to the name
"Not found", and the `TrafficDropped` condition would be marked as True,
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
    # reason is for machine consumption, message is for human consumption
    message: "Revision 'qyzz' referenced in rollout.traffic not found"
```


## Configuration not found by Route

If a Route references the `latestReadyRevisionName` of a Configuration
and the Configuration cannot be found, the corresponding entry in
`status.traffic` list will be set to the name "Not found", and the
`TrafficDropped` condition would be marked as True with a reason of
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
    # reason is for machine consumption, message is for human consumption
    message: "Revision 'my-service' referenced in rollout.traffic not found"
```


## Latest Revision of a Configuration deleted

If the most recent (or most recently ready) Revision is deleted, the
Configuration will clear the `latestReadyRevisionName`. If the
Configuration is referenced by a Route, then the Route will set the
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
  observedGeneration: 1234
```


## Resource exhausted while creating a revision

Since a Revision is only metadata, the Revision will be created, but
may have a condition indicating the underlying failure, possibly
including the associated underlying resource. In a multitenant
environment, the customer may not have have access or visibility into
the underlying resources in the hosting environment.

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
Revisions, we would start by assuming a single timeout for deployment
(rather than configurable), and report that the Revision was not
Ready, with a reason `ProgressDeadlineExceeded`. Note that we would only
report `ProgressDeadlineExceeded` if we could not determine another
reason (such as quota failures, missing build, or container execution
failures).

Kubernetes controllers will continue attempting to make progress
(possibly at a less-aggressive rate) when they encounter a case where
the desired status cannot match the actual status, so if the
underlying deployment is slow, it may eventually finish after
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
gradual rollout or knife-switch) takes longer than a certain timeout
to complete/update, the `RolloutInProgress` condition would remain at
true, but the reason would be set to `ProgressDeadlineExceeded`.

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
    # reason is for machine consumption, message is for human consumption
    message: "Unable to update traffic split for more than 120 seconds."
```


## Container image not present in repository

We expect that Revisions may be created while a Build is still
creating the container image or uploading it to the repository. If the
build is being performed by a CRD in the cluster, the spec.buildName
attribute will be set (and see the "Build failed" example). In other
cases when the build is not supplied, the container image referenced
may not be present in the registry (either because of a typo or
because it was deleted). In this case, the Ready condition will be set
to False with a reason of ContainerMissing. This condition could be
corrected if the image becomes available at a later time. We may also
make a defensive copy of the container image (e.g. in Riptide) to
avoid this error due to deleted source container.

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
