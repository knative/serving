# Knative Serving API spec

This file contains the [resource paths](#resource-paths) and [yaml
schemas](#resource-yaml-definitions) that make up the Knative Serving API.

## Resource Paths

Resource paths in the Knative Serving API have the following standard k8s form:

```
/apis/{apiGroup}/{apiVersion}/namespaces/{metadata.namespace}/{kind}/{metadata.name}
```

For example:

```
/apis/serving.knative.dev/v1alpha1/namespaces/default/routes/my-service
```

It is expected that each Route will provide a name within a
cluster-wide DNS name. While no particular URL scheme is mandated
(consult the `domain` property of the Route for the authoritative
mapping), a common implementation would be to use the kubernetes
namespace mechanism to produce a URL like the following:

```
[$revisionname].$route.$namespace.<common knative cluster suffix>
```

For example:

```
prod.my-service.default.mydomain.com
```


## Resource YAML Definitions

YAMLs for the Knative Serving API resources are described below, describing the
basic k8s structure: metadata, spec and status, along with comments on
specific fields.

### Route

For a high-level description of Routes,
[see the overview](overview.md#route).

```yaml
apiVersion: serving.knative.dev/v1alpha1
kind: Route
metadata:
  name: my-service
  namespace: default
  labels:
    knative.dev/type: ...  # +optional convention: function|app

  # system generated meta
  uid: ...
  resourceVersion: ...  # used for optimistic concurrency control
  creationTimestamp: ...
  generation: ...  # updated only when spec changes; used by observedGeneration
  selfLink: ...
  ...
spec:
  traffic:
  # list of oneof configurationName | revisionName.
  #  configurationName watches configurations to address latest latestReadyRevisionName
  #  revisionName pins a specific revision
  - configurationName: ...
    name: ...  # +optional. Access as {name}.${status.domain},
               #  e.g. oss: current.my-service.default.mydomain.com
    percent: 100  # list percentages must add to 100. 0 is a valid list value
  - ...

status:
  # domain: The hostname used to access the default (traffic-split)
  #   route. Typically, this will be composed of the name and namespace
  #   along with a cluster-specific prefix (here, mydomain.com).
  domain: my-service.default.mydomain.com

  # domainInternal: A DNS name for the default (traffic-split) route which can
  # be accessed without leaving the cluster environment.
  domainInternal: my-service.default.svc.cluster.local

  traffic:
  # current rollout status list. configurationName references
  #   are dereferenced to latest revision
  - revisionName: ...  # latestReadyRevisionName from a configurationName in spec
    name: ...
    percent: ...  # percentages add to 100. 0 is a valid list value
  - ...

  conditions:  # See also the [error conditions documentation](errors.md)
  - type: Ready
    status: True
  - type: AllTrafficAssigned
    status: True
  - ...

  observedGeneration: ...  # last generation being reconciled
```


### Configuration

For a high-level description of Configurations,
[see the overview](overview.md#configuration).


```yaml
apiVersion: serving.knative.dev/v1alpha1
kind: Configuration
metadata:
  name: my-service
  namespace: default

  # system generated meta
  uid: ...
  resourceVersion: ...  # used for optimistic concurrency control
  creationTimestamp: ...
  generation: ...  # updated only when spec changes; used by observedGeneration
  selfLink: ...
  ...
spec:
  # +optional. composable Build spec, if omitted provide image directly
  build:  # This is a build.knative.dev/v1alpha1.BuildTemplateSpec
    source:
      # oneof git|gcs|custom:

      # +optional.
      git:
        url: https://github.com/jrandom/myrepo
        commit: deadbeef  # Or branch, tag, ref

      # +optional. A zip archive or a manifest file in Google Cloud
      # Storage. A manifest file is a file containing a list of file
      # paths, backing URLs, and sha checksums. Manifest may be a more
      # efficient mechanism for a client to perform partial upload.
      gcs:
        location: https://...
        type: 'archive'  # Or 'manifest'

      # +optional. Custom specifies a container which will be run as
      # the first build step to fetch the source.
      custom:  # is a core.v1.Container
        image: gcr.io/cloud-builders/git:latest
        args: [ "clone", "https://...", "other-place" ]

    template:  # build template reference and arguments.
      name: go_1_9_fn  # builder name. Functions may have custom builders
      namespace: build-templates
      arguments:
      - name: _IMAGE
        value: gcr.io/...  # destination for image
      - name: _ENTRY_POINT
        value: index  # if function, language dependent entrypoint

  revisionTemplate:  # template for building Revision
    metadata: ...
      labels:
        knative.dev/type: "function"  # One of "function" or "app"
    spec:  # knative.RevisionTemplateSpec. Copied to a new revision

      # +optional. if rolling back, the client may set this to the
      #   previous  revision's build to avoid triggering a rebuild
      buildName: ...

      # is a core.v1.Container; some fields not allowed, such as resources, ports
      container:
        # image either provided as pre-built container, or built by Knative Serving from
        # source. When built by knative, set to the same as build template, e.g.
        # build.template.arguments[_IMAGE], as the "promise" of a future build.
        # If buildName is provided, it is expected that this image will be
        # present when the referenced build is complete.
        image: gcr.io/...
        command: ['run']
        args: []
        env:
        # list of environment vars
        - name: FOO
          value: bar
        - name: HELLO
          value: world
        - ...
        livenessProbe: ...  # Optional
        readinessProbe: ...  # Optional

      # +optional concurrency strategy.  Defaults to Multi.
      concurrencyModel: ...
      # +optional. max time the instance is allowed for responding to a request
      timeoutSeconds: ...
      serviceAccountName: ...  # Name of the service account the code should run as.

status:
  # the latest created and ready to serve. Watched by Route
  latestReadyRevisionName: abc
  # latest created revision, may still be in the process of being materialized
  latestCreatedRevisionName: def
  conditions:  # See also the [error conditions documentation](errors.md)
  - type: Ready
    status: False
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
  observedGeneration: ...  # last generation being reconciled
```


### Revision

For a high-level description of Revisions,
[see the overview](overview.md#revision).

```yaml
apiVersion: serving.knative.dev/v1alpha1
kind: Revision
metadata:
  name: myservice-a1e34  # system generated
  namespace: default
  labels:
    knative.dev/configuration: ...  # to list configurations/revisions by service
    knative.dev/type: "function"  # convention, one of "function" or "app"
    knative.dev/revision: ... # generated revision name
    knative.dev/revisionUID: ... # generated revision UID
  annotations:
    knative.dev/configurationGeneration: ...  # generation of configuration that created this Revision
  # system generated meta
  uid: ...
  resourceVersion: ...  # used for optimistic concurrency control
  creationTimestamp: ...
  generation: ...
  selfLink: ...
  ...

# spec populated by Configuration
spec:
  # +optional. name of the build.knative.dev/v1alpha1.Build if built from source
  buildName: ...

  container:  # corev1.Container
    # We disallow the following fields from corev1.Container:
    #  name, resources, ports, and volumeMounts
    image: gcr.io/...
    command: ['run']
    args: []
    env:  # list of environment vars
    - name: FOO
      value: bar
    - name: HELLO
      value: world
    - ...
    livenessProbe: ...  # Optional
    readinessProbe: ...  # Optional

  # Name of the service account the code should run as.
  serviceAccountName: ...

  # The Revision's level of readiness for receiving traffic.
  # This may not be specified at creation (defaults to Active),
  # and is used by the controllers and activator to enable
  # scaling to/from 0.
  servingState: Active | Reserve | Retired

  # Some function or server frameworks or application code may be written to
  # expect that each request will be granted a single-tenant process to run
  # (i.e. that the request code is run single-threaded).
  concurrencyModel: Single | Multi

  # NYI: https://github.com/knative/serving/issues/457
  # Many higher-level systems impose a per-request response deadline.
  timeoutSeconds: ...

status:
  # This is a copy of metadata from the container image or grafeas,
  # indicating the provenance of the revision. This is based on the
  # container image, but may need further clarification.
  imageSource:
    git|gcs: ...

  conditions:  # See also the documentation in errors.md
   - type: Ready
     status: False
     message: "Starting Instances"
  # If spec.buildName is provided
  - type: BuildComplete
    status: True
  # other conditions indicating build failure, if applicable
  - ...

  # URL for accessing the logs generated by this specific revision.
  # Note that logs may still be access controlled separately from
  # access to the API object.
  logUrl: "http://logging.infra.mycompany.com/...?filter=revision_uid=a1e34&..."

  # serviceName: The name for the core Kubernetes Service that fronts this
  #   revision. Typically, the name will be the same as the name of the
  #   revision.
  serviceName: myservice-a1e34
```


## Service

For a high-level description of Services,
[see the overview](overview.md#service).


```yaml
apiVersion: serving.knative.dev/v1alpha1
kind: :
metadata:
  name: myservice
  namespace: default
  labels:
    knative.dev/type: "function"  # convention, one of "function" or "app"
  # system generated meta
  uid: ...
  resourceVersion: ...  # used for optimistic concurrency control
  creationTimestamp: ...
  generation: ...
  selfLink: ...
  ...

# spec contains one of several possible rollout styles
spec:  # One of "runLatest" or "pinned"
  # Example, only one of runLatest or pinned can be set in practice.
  runLatest:
    configuration:  # serving.knative.dev/v1alpha1.Configuration
      # +optional. name of the build.knative.dev/v1alpha1.Build if built from source
      buildName: ...

      container:  # core.v1.Container
        image: gcr.io/...
        command: ['run']
        args: []
        env:  # list of environment vars
        - name: FOO
          value: bar
        - name: HELLO
          value: world
        - ...
        livenessProbe: ...  # Optional
        readinessProbe: ...  # Optional
      concurrencyModel: ...
      timeoutSeconds: ...
      serviceAccountName: ...  # Name of the service account the code should run as
  # Example, only one of runLatest or pinned can be set in practice.
  pinned:
    revisionName: myservice-00013  # Auto-generated revision name
    configuration:  # serving.knative.dev/v1alpha1.Configuration
      # +optional. name of the build.knative.dev/v1alpha1.Build if built from source
      buildName: ...

      container:  # core.v1.Container
        image: gcr.io/...
        command: ['run']
        args: []
        env:  # list of environment vars
        - name: FOO
          value: bar
        - name: HELLO
          value: world
        - ...
        livenessProbe: ...  # Optional
        readinessProbe: ...  # Optional
      concurrencyModel: ...
      timeoutSeconds: ...
      serviceAccountName: ...  # Name of the service account the code should run as
status:
  # This information is copied from the owned Configuration and Route.

  # The latest created and ready to serve Revision.
  latestReadyRevisionName: abc
  # Latest created Revision, may still be in the process of being materialized.
  latestCreatedRevisionName: def

  # domain: The hostname used to access the default (traffic-split)
  #   route. Typically, this will be composed of the name and namespace
  #   along with a cluster-specific prefix (here, mydomain.com).
  domain: myservice.default.mydomain.com

  # domainInternal: A DNS name for the default (traffic-split) route which can
  # be accessed without leaving the cluster environment.
  domainInternal: myservice.default.svc.cluster.local

  # current rollout status list. configurationName references
  #   are dereferenced to latest revision
  traffic:
  - revisionName: ...  # latestReadyRevisionName from a configurationName in spec
    name: ...
    percent: ...  # percentages add to 100. 0 is a valid list value
  - ...

  conditions:  # See also the documentation in errors.md
  - type: Ready
    status: False
    reason: RevisionMissing
    message: "Revision 'qyzz' referenced in traffic not found"
  - type: ConfigurationsReady
    status: False
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
  - type: RoutesReady
    status: False
    reason: RevisionMissing
    message: "Revision 'qyzz' referenced in traffic not found"

  observedGeneration: ...  # last generation being reconciled
```
