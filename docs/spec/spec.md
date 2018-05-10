# Elafros API spec

This file contains the [resource paths](#resource-paths) and [yaml
schemas](#resource-yaml-definitions) that make up the Elafros API.

## Resource Paths

Resource paths in the Elafros API have the following standard k8s form:

```
/apis/{apiGroup}/{apiVersion}/namespaces/{metadata.namespace}/{kind}/{metadata.name}
```

For example:

```
/apis/elafros.dev/v1alpha1/namespaces/default/routes/my-service
```

It is expected that each Route will provide a name within a
cluster-wide DNS name. While no particular URL scheme is mandated
(consult the `domain` property of the Route for the authoritative
mapping), a common implementation would be to use the kubernetes
namespace mechanism to produce a URL like the following:

```
[$revisionname].$route.$namespace.<common elafros cluster suffix>
```

For example:

```
prod.my-service.default.mydomain.com
```


## Resource YAML Definitions

YAMLs for the Elafros API resources are described below, describing the
basic k8s structure: metadata, spec and status, along with comments on
specific fields.

### Route

For a high-level description of Routes,
[see the overview](overview.md#route).

```yaml
apiVersion: elafros.dev/v1alpha1
kind: Route
metadata:
  name: my-service
  namespace: default
  labels:
    elafros.dev/type: ...  # +optional convention: function|app
 
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

  traffic:
  # current rollout status list. configurationName references
  #   are dereferenced to latest revision
  - revisionName: ...  # latestReadyRevisionName from a configurationName in spec
    name: ...
    percent: ...  # percentages add to 100. 0 is a valid list value
  - ...

  conditions:  # See also the [error conditions documentation](errors.md)
  - type: RolloutComplete
    status: True
  - type: TrafficDropped
    status: False
  - ...

  observedGeneration: ...  # last generation being reconciled
```


### Configuration

For a high-level description of Configurations,
[see the overview](overview.md#configuration).


```yaml
apiVersion: elafros.dev/v1alpha1
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
  build:  # This is a elafros.dev/v1alpha1.BuildTemplateSpec
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
        elafros.dev/type: "function"  # One of "function" or "app"
    spec:  # elafros.RevisionTemplateSpec. Copied to a new revision

      # +optional. if rolling back, the client may set this to the
      #   previous  revision's build to avoid triggering a rebuild
      buildName: ...

      # is a core.v1.Container; some fields not allowed, such as resources, ports
      container:
        # image either provided as pre-built container, or built by Elafros from
        # source. When built by elafros, set to the same as build template, e.g. 
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
  - type: LatestRevisionReady
    status: False
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."
  observedGeneration: ...  # last generation being reconciled
```


### Revision

For a high-level description of Revisions,
[see the overview](overview.md#revision).

```yaml
apiVersion: elafros.dev/v1alpha1
kind: Revision
metadata:
  name: myservice-a1e34  # system generated
  namespace: default
  labels:
    elafros.dev/configuration: ...  # to list configurations/revisions by service
    elafros.dev/type: "function"  # convention, one of "function" or "app"
    elafros.dev/revision: ... # generated revision name
    elafros.dev/revisionUID: ... # generated revision UID
  annotations:
    elafros.dev/configurationGeneration: ...  # generation of configuration that created this Revision
  # system generated meta
  uid: ...
  resourceVersion: ...  # used for optimistic concurrency control
  creationTimestamp: ...
  generation: ...
  selfLink: ...
  ...

# spec populated by Configuration
spec:
  # +optional. name of the elafros.dev/v1alpha1.Build if built from source
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

  # NYI: https://github.com/elafros/elafros/issues/457
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
```


## Service

For a high-level description of Services,
[see the overview](overview.md#service).


```yaml
apiVersion: elafros.dev/v1alpha1
kind: :
metadata:
  name: myservice
  namespace: default
  labels:
    elafros.dev/type: "function"  # convention, one of "function" or "app"
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
    configuration:  # elafros.dev/v1alpha1.Configuration
      # +optional. name of the elafros.dev/v1alpha1.Build if built from source
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
    configuration:  # elafros.dev/v1alpha1.Configuration
      # +optional. name of the elafros.dev/v1alpha1.Build if built from source
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
  domain: my-service.default.mydomain.com

  conditions:  # See also the documentation in errors.md
  - type: Ready
    status: True
    message: "Revision starting"
  - type: LatestRevisionReady
    status: False
    reason: ContainerMissing
    message: "Unable to start because container is missing and build failed."

  observedGeneration: ...  # last generation being reconciled
```
