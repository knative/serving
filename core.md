# Knative Serving — Core Resource Reconciliation Loop

A complete, from-first-principles guide to how a `Service` becomes running pods.

---

## Table of Contents

1. [First Principles — The Reconciliation Loop](#1-first-principles--the-reconciliation-loop)
2. [Resource Hierarchy & Type Definitions](#2-resource-hierarchy--type-definitions)
3. [Condition / Status System](#3-condition--status-system)
4. [Controller Bootstrap](#4-controller-bootstrap)
5. [Service Reconciler](#5-service-reconciler)
6. [Configuration Reconciler](#6-configuration-reconciler)
7. [Revision Reconciler](#7-revision-reconciler)
8. [Route Reconciler](#8-route-reconciler)
9. [End-to-End Walkthrough](#9-end-to-end-walkthrough)
10. [Ownership, Generations & Idempotency](#10-ownership-generations--idempotency)

---

## 1. First Principles — The Reconciliation Loop

### 1.1 The Core Problem Kubernetes Solves

Kubernetes manages distributed systems by storing **desired state** (what you want) in etcd and continuously working to make **actual state** (what exists) match it. The agents that do this work are called **controllers**.

A controller is a process that runs an infinite loop:

```
while true:
    desired  = read desired state from API server
    actual   = observe actual state (pods, deployments, etc.)
    if desired != actual:
        take action to reconcile the difference
```

This is called the **reconciliation loop** or **control loop**. It is the fundamental operating principle of everything in Kubernetes — and everything in Knative Serving.

### 1.2 Why Level-Triggered, Not Edge-Triggered

A naive implementation would react to events: "a Service was created, so create a Configuration now." Kubernetes controllers deliberately do NOT work this way. Instead they are **level-triggered**:

- On every reconcile call, the controller reads the full current state and computes what actions are needed.
- It doesn't matter whether reconciliation was triggered by a create, update, delete, or a timer — the logic is identical.
- If an action fails (network timeout, conflict), the item is simply re-queued and the full reconcile runs again.

This makes controllers **idempotent** and **self-healing**. A missed event, a crash, or a partial failure never permanently corrupts state — the next reconcile loop corrects it.

### 1.3 The Three Primitives: Informer, Lister, Work Queue

Every Knative controller is built on three client-go primitives:

```
┌─────────────────────────────────────────────────────────────────┐
│                         API Server                              │
└──────────────────────────┬──────────────────────────────────────┘
                           │ Watch (streaming events)
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Informer                                 │
│                                                                 │
│  - Opens a long-lived Watch connection to the API server        │
│  - Receives Add / Update / Delete events                        │
│  - Writes objects into a local in-memory store (the cache)      │
│  - Calls registered EventHandlers on each event                 │
└──────────┬──────────────────────────────────┬───────────────────┘
           │ Populates                         │ Triggers
           ▼                                  ▼
┌──────────────────────┐          ┌───────────────────────────────┐
│       Lister         │          │         Work Queue            │
│                      │          │                               │
│  Thread-safe read    │          │  Rate-limited FIFO queue of   │
│  access to the       │          │  "namespace/name" keys.       │
│  in-memory cache.    │          │  Deduplicates: if the same    │
│  No API calls.       │          │  key is added 10x while a     │
│  O(1) lookups.       │          │  worker is busy, it only      │
└──────────────────────┘          │  processes once.              │
                                  └──────────────┬────────────────┘
                                                 │ Dequeued by
                                                 ▼
                                  ┌───────────────────────────────┐
                                  │       Worker Goroutines       │
                                  │                               │
                                  │  Pull key from queue →        │
                                  │  fetch full object from       │
                                  │  Lister → call ReconcileKind  │
                                  └───────────────────────────────┘
```

**Why use a Lister instead of calling the API server directly?**

The Lister reads from the local in-memory cache that the Informer keeps up to date. This means:
- No extra API server load per reconcile
- Sub-millisecond lookups
- The cache is eventually consistent — you may see slightly stale data, but the next watch event will re-trigger reconciliation

### 1.4 What Happens When an Object Changes

Here is the exact sequence when you run `kubectl apply -f my-service.yaml`:

```
1. kubectl sends a PUT/POST to the API server
2. API server writes the new object to etcd
3. API server emits a Watch event (ADDED or MODIFIED)
4. The Service Informer receives the event
5. The Informer updates its local cache
6. The Informer calls all registered EventHandlers
7. The EventHandler calls impl.Enqueue(obj), which puts
   "namespace/service-name" onto the work queue
8. A worker goroutine picks up the key
9. The worker calls ReconcileKind(ctx, service)
10. ReconcileKind reads desired state, compares with actual,
    creates/updates child resources as needed
11. The worker calls c.client.ServingV1().Services(...).UpdateStatus(...)
    to persist the updated .Status back to the API server
```

Steps 1–11 happen for **every change** to the object. If step 10 creates a new Configuration, that triggers its own Informer event on the Configuration informer, which triggers the Configuration reconciler, and so on down the chain.

### 1.5 The ReconcileKind Interface

Knative's code generation (via `knative.dev/pkg/codegen`) produces a reconciler framework that handles the boilerplate: fetching the object, handling not-found errors, calling finalizers, updating status. The only thing a developer writes is `ReconcileKind`.

The generated interface that every reconciler must satisfy:

```go
// Generated by code generation — do not edit directly.
// Lives in pkg/client/injection/reconciler/serving/v1/service/reconciler.go
type Interface interface {
    ReconcileKind(ctx context.Context, o *v1.Service) reconciler.Event
}
```

The generated reconciler wrapper:
1. Fetches the object from the lister (cache)
2. Calls `ReconcileKind` with a deep copy
3. If `ReconcileKind` returns nil — calls `UpdateStatus` if status changed
4. If `ReconcileKind` returns a permanent error — marks it terminal, does not re-queue
5. If `ReconcileKind` returns a transient error — re-queues with backoff

This is why every `ReconcileKind` in Knative Serving has this pattern at the top:

```go
// pkg/reconciler/service/service.go:72-74
func (c *Reconciler) ReconcileKind(ctx context.Context, service *v1.Service) pkgreconciler.Event {
    ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
    defer cancel()
    // ...
}
```

The timeout ensures a stuck reconcile (e.g. waiting on a slow API server) doesn't hold a worker goroutine forever.

### 1.6 How Child Resource Changes Bubble Up

A key insight: when the Service reconciler creates a Configuration, it needs to be re-triggered when that Configuration's status changes. It does this by registering an event handler on the Configuration informer that **enqueues the owner** (the Service) rather than the Configuration itself:

```go
// pkg/reconciler/service/controller.go:65-70
handleControllerOf := cache.FilteringResourceEventHandler{
    FilterFunc: controller.FilterController(&v1.Service{}),
    Handler:    controller.HandleAll(impl.EnqueueControllerOf),
}
configurationInformer.Informer().AddEventHandler(handleControllerOf)
routeInformer.Informer().AddEventHandler(handleControllerOf)
```

- `FilterController(&v1.Service{})` — only passes events for Configurations/Routes that are **owned by a Service** (checks OwnerReferences)
- `EnqueueControllerOf` — puts the **Service's** key (not the Configuration's key) onto the work queue

So the event flow for a Configuration status change is:

```
Configuration status updated
  → Configuration Informer fires
  → FilterController checks: does this Configuration have a Service owner? YES
  → EnqueueControllerOf puts "namespace/my-service" on the queue
  → Service ReconcileKind runs
  → Service reads the updated Configuration status and propagates it to its own status
```

This is how status changes propagate **bottom-up** through the hierarchy, even though resource creation flows **top-down**.

### 1.7 Summary of the Pattern

```
┌─────────────────────────────────────────────────────────────────┐
│  DESIRED STATE FLOWS DOWN       STATUS FLOWS UP                 │
│                                                                 │
│  User applies Service           Pod becomes Ready               │
│       │                              │                          │
│       ▼                              ▼                          │
│  Service reconciler          Deployment status updates          │
│  creates Configuration            │                             │
│       │                    Revision reconciler reads it         │
│       ▼                    → marks RevisionConditionReady=True  │
│  Configuration reconciler         │                             │
│  creates Revision          Configuration reconciler reads it    │
│       │                    → sets LatestReadyRevisionName       │
│       ▼                           │                             │
│  Revision reconciler       Service reconciler reads it          │
│  creates Deployment        → PropagateConfigurationStatus()     │
│       │                           │                             │
│       ▼                           ▼                             │
│  Kubernetes creates Pod    Service.Status.Ready = True          │
└─────────────────────────────────────────────────────────────────┘
```

Every layer only ever reads from its **lister cache** (no live API calls for reads) and writes only its **own status** and its **direct child resources**.

---

_Next: Section 2 — Resource Hierarchy & Type Definitions_

---

## 2. Resource Hierarchy & Type Definitions

### 2.1 The Four Core Resources

Knative Serving introduces four Custom Resource Definitions (CRDs) on top of Kubernetes. They sit in a strict parent-child hierarchy:

```
Service
├── Configuration        (owns: manages desired pod template)
│   └── Revision         (owns: a single immutable snapshot)
│       ├── Deployment   (Kubernetes native)
│       ├── PodAutoscaler
│       └── Image        (caching.knative.dev, optional)
└── Route                (owns: manages traffic distribution)
    └── Ingress          (networking.knative.dev, programs the ingress layer)
```

All four live in [pkg/apis/serving/v1/](pkg/apis/serving/v1/).

A critical design rule: **every resource only manages its direct children**. The Service reconciler never touches a Deployment. The Configuration reconciler never touches an Ingress. This strict layering makes each controller small, testable, and independently replaceable.

### 2.2 Service

**File:** [pkg/apis/serving/v1/service_types.go](pkg/apis/serving/v1/service_types.go)

```go
// service_types.go:43-53
type Service struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ServiceSpec   `json:"spec,omitempty"`
    Status ServiceStatus `json:"status,omitempty"`
}
```

`ServiceSpec` is intentionally just an inline composition of the two child specs:

```go
// service_types.go:78-87
type ServiceSpec struct {
    ConfigurationSpec `json:",inline"`  // the pod template
    RouteSpec         `json:",inline"`  // the traffic rules
}
```

This means when you write a `Service` YAML, you're actually writing a `ConfigurationSpec` (your container image, env vars, resources) and a `RouteSpec` (your traffic splits) in one place. The Service reconciler's job is to split this into a real `Configuration` and a real `Route`.

`ServiceStatus` aggregates the status of both children:

```go
// service_types.go:117-127
type ServiceStatus struct {
    duckv1.Status             `json:",inline"`  // holds Conditions[]
    ConfigurationStatusFields `json:",inline"`  // LatestReadyRevisionName, LatestCreatedRevisionName
    RouteStatusFields         `json:",inline"`  // URL, Address, Traffic[]
}
```

**Service Conditions:**

```go
// service_types.go:93-101
ServiceConditionReady                = apis.ConditionReady         // "Ready"
ServiceConditionRoutesReady          apis.ConditionType = "RoutesReady"
ServiceConditionConfigurationsReady  apis.ConditionType = "ConfigurationsReady"
```

`ServiceConditionReady` is automatically managed by the condition framework — it becomes `True` only when **both** `ConfigurationsReady` and `RoutesReady` are `True`. You never set it manually.

**Resource creation:** The Service reconciler creates its children using two factory functions:

```go
// pkg/reconciler/service/resources/configuration.go:35-68
func MakeConfiguration(service *v1.Service) *v1.Configuration {
    return MakeConfigurationFromExisting(service, &v1.Configuration{})
}

func MakeConfigurationFromExisting(service *v1.Service, existing *v1.Configuration) *v1.Configuration {
    return &v1.Configuration{
        ObjectMeta: metav1.ObjectMeta{
            Name:      names.Configuration(service),  // same name as the Service
            Namespace: service.Namespace,
            OwnerReferences: []metav1.OwnerReference{
                *kmeta.NewControllerRef(service),     // Service owns Configuration
            },
            Labels:      kmeta.UnionMaps(service.GetLabels(), labels),
            Annotations: anns,
        },
        Spec: service.Spec.ConfigurationSpec,  // just copy the pod template part
    }
}
```

```go
// pkg/reconciler/service/resources/route.go:30-58
func MakeRoute(service *v1.Service) *v1.Route {
    c := &v1.Route{
        ObjectMeta: metav1.ObjectMeta{
            Name:      names.Route(service),  // same name as the Service
            Namespace: service.Namespace,
            OwnerReferences: []metav1.OwnerReference{
                *kmeta.NewControllerRef(service),  // Service owns Route
            },
            // ...labels and annotations...
        },
        Spec: *service.Spec.RouteSpec.DeepCopy(),
    }
    // Fill in ConfigurationName for any traffic target that has no explicit RevisionName
    for idx := range c.Spec.Traffic {
        if c.Spec.Traffic[idx].RevisionName == "" {
            c.Spec.Traffic[idx].ConfigurationName = names.Configuration(service)
        }
    }
    return c
}
```

Notice the last loop: if a traffic target in the Service's RouteSpec has no `revisionName`, it gets the Configuration name filled in. This is how "send 100% traffic to latest" works — the Route's traffic target points to the Configuration by name, and the Route reconciler resolves that to `LatestReadyRevisionName` at reconcile time.

### 2.3 Configuration

**File:** [pkg/apis/serving/v1/configuration_types.go](pkg/apis/serving/v1/configuration_types.go)

```go
// configuration_types.go:35-45
type Configuration struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ConfigurationSpec   `json:"spec,omitempty"`
    Status ConfigurationStatus `json:"status,omitempty"`
}
```

`ConfigurationSpec` holds exactly one thing — the template for the next Revision:

```go
// configuration_types.go:64-68
type ConfigurationSpec struct {
    Template RevisionTemplateSpec `json:"template"`
}
```

Every time you update `ConfigurationSpec.Template`, Kubernetes increments `Configuration.Generation`. The Configuration reconciler detects this and stamps out a new `Revision`.

`ConfigurationStatusFields` is defined separately (and inlined in both `ConfigurationStatus` and `ServiceStatus`) because the Service needs these fields too:

```go
// configuration_types.go:84-94
type ConfigurationStatusFields struct {
    // The most recent Revision that became Ready.
    LatestReadyRevisionName string `json:"latestReadyRevisionName,omitempty"`

    // The most recently created Revision (may not be ready yet).
    LatestCreatedRevisionName string `json:"latestCreatedRevisionName,omitempty"`
}
```

The distinction between these two fields is important:
- `LatestCreatedRevisionName` = "I just created this Revision, it may still be pulling the image"
- `LatestReadyRevisionName` = "This Revision's pods are healthy and ready to serve traffic"

Traffic only flows to `LatestReadyRevisionName`. `LatestCreatedRevisionName` can be a Revision that is failing.

**Configuration Condition:**

```go
// configuration_types.go:73
ConfigurationConditionReady = apis.ConditionReady  // "Ready"
```

Configuration only has one condition. It is `True` when `LatestReadyRevisionName` is up to date and the underlying Revision is ready.

### 2.4 Revision

**File:** [pkg/apis/serving/v1/revision_types.go](pkg/apis/serving/v1/revision_types.go)

```go
// revision_types.go:36-46
type Revision struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   RevisionSpec   `json:"spec,omitempty"`
    Status RevisionStatus `json:"status,omitempty"`
}
```

A Revision is **immutable after creation**. You never update `Revision.Spec`. This is the foundation of Knative's traffic splitting model — every distinct pod template is a different Revision, and you can route percentages of traffic to any of them simultaneously.

`RevisionSpec` embeds Kubernetes' full `corev1.PodSpec` plus four Knative-specific fields:

```go
// revision_types.go:76-103
type RevisionSpec struct {
    corev1.PodSpec `json:",inline"`  // containers, volumes, serviceAccountName, etc.

    // Max concurrent requests per container instance. 0 = unlimited.
    ContainerConcurrency *int64 `json:"containerConcurrency,omitempty"`

    // Max time for the request to complete.
    TimeoutSeconds *int64 `json:"timeoutSeconds,omitempty"`

    // Max time until the first byte of the response.
    ResponseStartTimeoutSeconds *int64 `json:"responseStartTimeoutSeconds,omitempty"`

    // Max idle time (no bytes flowing) before the request is cut.
    IdleTimeoutSeconds *int64 `json:"idleTimeoutSeconds,omitempty"`
}
```

`RevisionStatus` tracks what the Revision reconciler has done:

```go
// revision_types.go:135-167
type RevisionStatus struct {
    duckv1.Status `json:",inline"`  // holds Conditions[]

    LogURL string `json:"logUrl,omitempty"`  // link to logs for this revision

    // Resolved image digests, filled after digest resolution completes
    ContainerStatuses     []ContainerStatus `json:"containerStatuses,omitempty"`
    InitContainerStatuses []ContainerStatus `json:"initContainerStatuses,omitempty"`

    // Live replica counts, sourced from the PodAutoscaler
    ActualReplicas  *int32 `json:"actualReplicas,omitempty"`
    DesiredReplicas *int32 `json:"desiredReplicas,omitempty"`
}
```

**Revision Conditions:**

```go
// revision_types.go:108-118
RevisionConditionReady               = apis.ConditionReady          // "Ready"
RevisionConditionResourcesAvailable  apis.ConditionType = "ResourcesAvailable"
RevisionConditionContainerHealthy    apis.ConditionType = "ContainerHealthy"
RevisionConditionActive              apis.ConditionType = "Active"
```

`RevisionConditionReady` is the AND of `ResourcesAvailable` and `ContainerHealthy`:
- **`ResourcesAvailable`** — the Deployment has been created and pods are scheduled
- **`ContainerHealthy`** — at least one pod's container is running and passing health checks
- **`Active`** — the revision has non-zero replica count (NOT part of the Ready condition set — a revision can be `Ready=True` even when scaled to zero)

### 2.5 Route

**File:** [pkg/apis/serving/v1/route_types.go](pkg/apis/serving/v1/route_types.go)

```go
// route_types.go:37-49
type Route struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   RouteSpec   `json:"spec,omitempty"`
    Status RouteStatus `json:"status,omitempty"`
}
```

`RouteSpec` holds the traffic distribution table:

```go
// route_types.go:114-119
type RouteSpec struct {
    Traffic []TrafficTarget `json:"traffic,omitempty"`
}
```

Each `TrafficTarget` entry routes a percentage of traffic to either a specific Revision or the latest ready Revision of a Configuration:

```go
// route_types.go:68-111
type TrafficTarget struct {
    Tag string `json:"tag,omitempty"`  // optional subdomain tag (e.g. "canary")

    // Point to a specific Revision. Mutually exclusive with ConfigurationName.
    RevisionName string `json:"revisionName,omitempty"`

    // Point to the latest ready Revision of a Configuration.
    // Mutually exclusive with RevisionName.
    ConfigurationName string `json:"configurationName,omitempty"`

    // Must be true when RevisionName is empty.
    LatestRevision *bool `json:"latestRevision,omitempty"`

    // Percent of traffic to send here. All entries must sum to 100.
    Percent *int64 `json:"percent,omitempty"`

    // Filled in by the controller in Status — the resolved URL for this target.
    URL *apis.URL `json:"url,omitempty"`
}
```

`RouteStatusFields` is again defined separately so `ServiceStatus` can inline it:

```go
// route_types.go:156-172
type RouteStatusFields struct {
    URL     *apis.URL          `json:"url,omitempty"`     // the main public URL
    Address *duckv1.Addressable `json:"address,omitempty"` // cluster-internal address
    Traffic []TrafficTarget    `json:"traffic,omitempty"` // resolved (RevisionName only, no ConfigurationName)
}
```

Note that in `RouteStatus.Traffic`, `ConfigurationName` entries are always **resolved** to concrete `RevisionName` entries. The status always shows exactly where traffic is currently going.

**Route Conditions:**

```go
// route_types.go:124-137
RouteConditionReady                  = apis.ConditionReady
RouteConditionAllTrafficAssigned     apis.ConditionType = "AllTrafficAssigned"
RouteConditionIngressReady           apis.ConditionType = "IngressReady"
RouteConditionCertificateProvisioned apis.ConditionType = "CertificateProvisioned"
```

`RouteConditionReady` = AND of `AllTrafficAssigned` + `IngressReady` + `CertificateProvisioned`.

### 2.6 How the Types Relate: Data Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│  User's YAML                                                         │
│                                                                      │
│  apiVersion: serving.knative.dev/v1                                  │
│  kind: Service                                                       │
│  spec:                                                               │
│    template:              ← ConfigurationSpec (pod template)         │
│      spec:                                                           │
│        containers:                                                   │
│        - image: myapp:v2                                             │
│    traffic:               ← RouteSpec (traffic rules)               │
│    - latestRevision: true                                            │
│      percent: 100                                                    │
└──────────────┬───────────────────────────────────────────────────────┘
               │ Service reconciler splits this into:
       ┌───────┴────────┐
       ▼                ▼
┌──────────────┐  ┌──────────────┐
│ Configuration│  │    Route     │
│              │  │              │
│ spec:        │  │ spec:        │
│  template:   │  │  traffic:    │
│   image: v2  │  │  - config:   │
│              │  │    myapp     │ ← ConfigurationName filled in by MakeRoute()
│ status:      │  │    pct: 100  │
│  latestReady:│  │              │
│   myapp-00002│  │ status:      │
│  latestCreat:│  │  url: http://│
│   myapp-00002│  │  traffic:    │
└──────┬───────┘  │  - revision: │
       │          │    myapp-002 │ ← Resolved by Route reconciler
       │          │    pct: 100  │
       ▼          └──────────────┘
┌──────────────┐
│  Revision    │  (immutable after creation)
│  myapp-00002 │
│              │
│ spec:        │
│  image: v2   │
│  concurrency:│
│   0          │
│              │
│ status:      │
│  ready: True │
│  actualRep: 2│
└──────┬───────┘
       │ Revision reconciler creates:
       ├──────────────────┐
       ▼                  ▼
┌──────────────┐  ┌──────────────┐
│  Deployment  │  │PodAutoscaler │
│  (k8s native)│  │ (KPA/HPA)    │
└──────┬───────┘  └──────────────┘
       │
       ▼
┌──────────────┐
│     Pod      │
│  myapp:v2    │
│  + queue-    │
│    proxy     │
└──────────────┘
```

### 2.7 Naming Convention

All child resources get the **same name** as their parent (where only one child of that type exists). This is enforced by the `names` package:

```
Service "myapp"
  → Configuration "myapp"       (names.Configuration = service.Name)
    → Revision "myapp-00001"    (kmeta.ChildName(config.Name, "-00001"))
      → Deployment "myapp-00001"
      → PodAutoscaler "myapp-00001"
  → Route "myapp"               (names.Route = service.Name)
```

This predictability means any controller can look up a child resource by name without a label query — it just uses `lister.Get(parentName)`.

### 2.8 All Types Are in the Same Package

All four resource types live together in [pkg/apis/serving/v1/](pkg/apis/serving/v1/). The package also contains:

| File | Purpose |
|------|---------|
| [`service_types.go`](pkg/apis/serving/v1/service_types.go) | `Service` struct + conditions |
| [`service_lifecycle.go`](pkg/apis/serving/v1/service_lifecycle.go) | `PropagateConfigurationStatus`, `PropagateRouteStatus`, mark helpers |
| [`configuration_types.go`](pkg/apis/serving/v1/configuration_types.go) | `Configuration` struct + conditions |
| [`configuration_lifecycle.go`](pkg/apis/serving/v1/configuration_lifecycle.go) | `SetLatestCreatedRevisionName`, `SetLatestReadyRevisionName`, mark helpers |
| [`revision_types.go`](pkg/apis/serving/v1/revision_types.go) | `Revision` struct + conditions |
| [`revision_lifecycle.go`](pkg/apis/serving/v1/revision_lifecycle.go) | `PropagateDeploymentStatus`, `PropagateAutoscalerStatus`, mark helpers |
| [`route_types.go`](pkg/apis/serving/v1/route_types.go) | `Route` struct + `TrafficTarget` + conditions |
| [`route_lifecycle.go`](pkg/apis/serving/v1/route_lifecycle.go) | `PropagateIngressStatus`, `MarkTrafficAssigned`, mark helpers |

The `*_lifecycle.go` files contain all the status manipulation logic. Reconcilers never set condition fields directly — they always call these lifecycle methods. This is covered in depth in Section 3.

---

_Next: Section 3 — Condition / Status System_

---

## 3. Condition / Status System

### 3.1 What Is a Condition?

Every Knative resource carries a `[]Condition` slice inside `duckv1.Status`. A `Condition` is a named boolean with extra metadata:

```go
// From knative.dev/pkg/apis/condition_types.go
type Condition struct {
    Type               ConditionType          // e.g. "Ready", "ContainerHealthy"
    Status             corev1.ConditionStatus // "True", "False", or "Unknown"
    Severity           ConditionSeverity      // "" (error) or "Warning"
    LastTransitionTime VolatileTime           // when Status last changed
    Reason             string                 // machine-readable short reason
    Message            string                 // human-readable detail
}
```

Three values for `Status`:
- **`True`** — this aspect of the resource is healthy and working
- **`False`** — this aspect has permanently failed (requires intervention)
- **`Unknown`** — still working towards the goal, or waiting on a dependency

Every resource has a special `Ready` condition. It is computed automatically from all the other conditions — if any sub-condition is `False`, `Ready` becomes `False`. If any is `Unknown`, `Ready` becomes `Unknown`. `Ready` is `True` only when every sub-condition is `True`.

### 3.2 ConditionSet: Declaring What Conditions a Resource Has

Each resource type declares its conditions with `apis.NewLivingConditionSet`. This is what defines the AND gate for `Ready`.

```go
// revision_lifecycle.go:55-58
// "Active" is intentionally excluded — a scaled-to-zero revision can still be Ready
var revisionCondSet = apis.NewLivingConditionSet(
    RevisionConditionResourcesAvailable,
    RevisionConditionContainerHealthy,
)
```

```go
// service_lifecycle.go:33-36
var serviceCondSet = apis.NewLivingConditionSet(
    ServiceConditionConfigurationsReady,
    ServiceConditionRoutesReady,
)
```

```go
// route_lifecycle.go:31-35
var routeCondSet = apis.NewLivingConditionSet(
    RouteConditionAllTrafficAssigned,
    RouteConditionIngressReady,
    RouteConditionCertificateProvisioned,
)
```

```go
// configuration_lifecycle.go:25
// No extra conditions — only the single "Ready" condition
var configCondSet = apis.NewLivingConditionSet()
```

`NewLivingConditionSet` always implicitly includes `apis.ConditionReady` as the top-level aggregate. You pass in the _sub-conditions_ that feed into it.

The diagram for each resource:

```
Revision Ready = AND(ResourcesAvailable, ContainerHealthy)

Service Ready  = AND(ConfigurationsReady, RoutesReady)

Route Ready    = AND(AllTrafficAssigned, IngressReady, CertificateProvisioned)

Configuration Ready = just "Ready" (no sub-conditions, set directly)
```

### 3.3 How MarkTrue / MarkFalse / MarkUnknown Work

Reconcilers never touch `Status.Conditions` directly. They always call the lifecycle helper methods, which go through the `ConditionManager`:

```go
// revision_lifecycle.go:121-122
func (rs *RevisionStatus) MarkContainerHealthyTrue() {
    revisionCondSet.Manage(rs).MarkTrue(RevisionConditionContainerHealthy)
}

// revision_lifecycle.go:126-130
func (rs *RevisionStatus) MarkContainerHealthyFalse(reason, message string) {
    revisionCondSet.Manage(rs).MarkFalse(RevisionConditionContainerHealthy, reason, "%s", message)
}

// revision_lifecycle.go:133-137
func (rs *RevisionStatus) MarkContainerHealthyUnknown(reason, message string) {
    revisionCondSet.Manage(rs).MarkUnknown(RevisionConditionContainerHealthy, reason, "%s", message)
}
```

The `.Manage(rs)` call returns a `ConditionManager` bound to the Revision's status. When you call `MarkTrue(RevisionConditionContainerHealthy)` on it, the manager:
1. Sets `ContainerHealthy.Status = "True"`
2. Checks all other sub-conditions in `revisionCondSet`
3. If all are `True` → sets `Ready = True`
4. If any is `False` → sets `Ready = False`
5. If any is `Unknown` (and none are `False`) → sets `Ready = Unknown`

This cascade is automatic. Reconcilers only set the specific sub-condition they know about — the framework maintains `Ready` for them.

### 3.4 The ObservedGeneration Guard in IsReady()

`IsReady()` on every resource has this same two-part check:

```go
// revision_lifecycle.go:72-76
func (r *Revision) IsReady() bool {
    rs := r.Status
    return rs.ObservedGeneration == r.Generation &&
        rs.GetCondition(RevisionConditionReady).IsTrue()
}
```

```go
// configuration_lifecycle.go:39-43
func (c *Configuration) IsReady() bool {
    cs := c.Status
    return cs.ObservedGeneration == c.Generation &&
        cs.GetCondition(ConfigurationConditionReady).IsTrue()
}
```

Why check `ObservedGeneration == Generation`?

- `Generation` (in `ObjectMeta`) is incremented by the API server every time `Spec` changes.
- `ObservedGeneration` (in `Status`) is set by the reconciler framework after each successful `ReconcileKind` run.
- If they differ, the controller has not yet processed this version of the spec. The conditions in `.Status` reflect the **old** spec, not the current one.

Without this guard, a parent resource could see `Ready=True` from an old run and incorrectly assume the current spec was processed.

In practice this means: when you update a Service's image, its `Generation` immediately becomes `N+1`. Every `IsReady()` check returns `false` until the reconciler finishes processing the new image and sets `ObservedGeneration = N+1`.

### 3.5 Configuration's Dual-Pointer Status Fields

Configuration has two fields that are easy to confuse:

```go
// configuration_types.go:84-94
type ConfigurationStatusFields struct {
    LatestReadyRevisionName   string  // the newest revision that is Ready=True
    LatestCreatedRevisionName string  // the newest revision that was created
}
```

The lifecycle methods manage the relationship between them:

```go
// configuration_lifecycle.go:76-82
func (cs *ConfigurationStatus) SetLatestCreatedRevisionName(name string) {
    cs.LatestCreatedRevisionName = name
    if cs.LatestReadyRevisionName != name {
        // Created != Ready means we're waiting → mark as Unknown
        configCondSet.Manage(cs).MarkUnknown(ConfigurationConditionReady, "", "")
    }
}

// configuration_lifecycle.go:87-92
func (cs *ConfigurationStatus) SetLatestReadyRevisionName(name string) {
    cs.LatestReadyRevisionName = name
    if cs.LatestReadyRevisionName == cs.LatestCreatedRevisionName {
        // Ready caught up to Created → mark True
        configCondSet.Manage(cs).MarkTrue(ConfigurationConditionReady)
    }
}
```

State machine:

```
New revision created:
  SetLatestCreatedRevisionName("myapp-00003")
    → LatestCreated = "myapp-00003"
    → LatestReady  = "myapp-00002"   (old)
    → Created != Ready → ConfigurationReady = Unknown

Revision becomes Ready:
  SetLatestReadyRevisionName("myapp-00003")
    → LatestReady  = "myapp-00003"
    → LatestCreated = "myapp-00003"  (same)
    → Created == Ready → ConfigurationReady = True

Revision fails:
  MarkLatestCreatedFailed("myapp-00003", "OOMKilled")
    → ConfigurationReady = False, Reason="RevisionFailed"
```

### 3.6 Propagation: How Status Climbs Up the Hierarchy

No controller reads another controller's conditions directly. Instead, each parent controller calls a `Propagate*` method that copies the relevant condition from the child into its own status. These methods live in the `*_lifecycle.go` files.

#### Deployment → Revision

```go
// revision_lifecycle.go:156-169
func (rs *RevisionStatus) PropagateDeploymentStatus(original *appsv1.DeploymentStatus) {
    ds := serving.TransformDeploymentStatus(original)
    cond := ds.GetCondition(serving.DeploymentConditionReady)

    m := revisionCondSet.Manage(rs)
    switch cond.Status {
    case corev1.ConditionTrue:
        m.MarkTrue(RevisionConditionResourcesAvailable)
    case corev1.ConditionFalse:
        m.MarkFalse(RevisionConditionResourcesAvailable, cond.Reason, cond.Message)
    case corev1.ConditionUnknown:
        m.MarkUnknown(RevisionConditionResourcesAvailable, cond.Reason, cond.Message)
    }
}
```

The Deployment's ready condition maps to `RevisionConditionResourcesAvailable`. Note that the Revision reconciler reads the Deployment from its lister, then calls `PropagateDeploymentStatus` to copy it — the Deployment controller itself knows nothing about Revisions.

#### PodAutoscaler → Revision

```go
// revision_lifecycle.go:172-239
func (rs *RevisionStatus) PropagateAutoscalerStatus(ps *autoscalingv1alpha1.PodAutoscalerStatus) {
    // Copy replica counts
    rs.ActualReplicas = ps.ActualScale
    rs.DesiredReplicas = ps.DesiredScale

    cond := ps.GetCondition(autoscalingv1alpha1.PodAutoscalerConditionReady)

    // If PA scale target is initialized (pods have been ready at least once):
    if ps.IsScaleTargetInitialized() && !resUnavailable {
        rs.MarkResourcesAvailableTrue()
        rs.MarkContainerHealthyTrue()
    }

    switch cond.Status {
    case corev1.ConditionTrue:
        rs.MarkActiveTrue()
    case corev1.ConditionFalse:
        if !ps.IsScaleTargetInitialized() && ps.ServiceName != "" {
            rs.MarkResourcesAvailableFalse(ReasonProgressDeadlineExceeded,
                "Initial scale was never achieved")
        }
        rs.MarkActiveFalse(cond.Reason, cond.Message)
    case corev1.ConditionUnknown:
        rs.MarkActiveUnknown(cond.Reason, cond.Message)
    }
}
```

This is the most complex propagation. `Active=False` simply means the PA has scaled to zero, which is normal and does NOT make the Revision `Ready=False`. But if the scale target was **never** initialized (pods never became ready during the progress deadline), that marks `ResourcesAvailable=False`, which cascades to `Ready=False`.

#### Configuration → Service

```go
// service_lifecycle.go:92-107
func (ss *ServiceStatus) PropagateConfigurationStatus(cs *ConfigurationStatus) {
    // Copy the dual-pointer fields up to Service
    ss.ConfigurationStatusFields = cs.ConfigurationStatusFields

    cc := cs.GetCondition(ConfigurationConditionReady)
    if cc == nil {
        return
    }
    switch cc.Status {
    case corev1.ConditionUnknown:
        serviceCondSet.Manage(ss).MarkUnknown(ServiceConditionConfigurationsReady, cc.Reason, cc.Message)
    case corev1.ConditionTrue:
        serviceCondSet.Manage(ss).MarkTrue(ServiceConditionConfigurationsReady)
    case corev1.ConditionFalse:
        serviceCondSet.Manage(ss).MarkFalse(ServiceConditionConfigurationsReady, cc.Reason, cc.Message)
    }
}
```

Two things happen here:
1. `LatestReadyRevisionName` and `LatestCreatedRevisionName` are copied to `ServiceStatus` — this is how `kubectl get ksvc` shows you the current revision name.
2. `Configuration.Ready` → `Service.ConfigurationsReady` (which then contributes to `Service.Ready`).

#### Route → Service

```go
// service_lifecycle.go:130-147
func (ss *ServiceStatus) PropagateRouteStatus(rs *RouteStatus) {
    // Copy URL, Address, and resolved Traffic table
    ss.RouteStatusFields = rs.RouteStatusFields

    rc := rs.GetCondition(RouteConditionReady)
    switch rc.Status {
    case corev1.ConditionTrue:
        serviceCondSet.Manage(ss).MarkTrue(ServiceConditionRoutesReady)
    case corev1.ConditionFalse:
        serviceCondSet.Manage(ss).MarkFalse(ServiceConditionRoutesReady, rc.Reason, rc.Message)
    case corev1.ConditionUnknown:
        serviceCondSet.Manage(ss).MarkUnknown(ServiceConditionRoutesReady, rc.Reason, rc.Message)
    }
}
```

This is how `Service.Status.URL` gets populated — it's copied from `Route.Status.URL` via `RouteStatusFields`.

#### Ingress → Route

```go
// route_lifecycle.go:246-262
func (rs *RouteStatus) PropagateIngressStatus(cs v1alpha1.IngressStatus) {
    cc := cs.GetCondition(v1alpha1.IngressConditionReady)
    if cc == nil {
        rs.MarkIngressNotConfigured()
        return
    }
    m := routeCondSet.Manage(rs)
    switch cc.Status {
    case corev1.ConditionTrue:
        m.MarkTrue(RouteConditionIngressReady)
    case corev1.ConditionFalse:
        m.MarkFalse(RouteConditionIngressReady, cc.Reason, cc.Message)
    case corev1.ConditionUnknown:
        m.MarkUnknown(RouteConditionIngressReady, cc.Reason, cc.Message)
    }
}
```

### 3.7 Complete Condition Cascade Diagram

Putting it all together — this is the full condition propagation chain from a running Pod back up to the Service:

```
Pod running & passing health checks
          │
          │ (Kubernetes sets Deployment.Status.AvailableReplicas > 0)
          ▼
Deployment.Status
  Conditions:
  - Available = True
          │
          │ PropagateDeploymentStatus()
          ▼
Revision.Status
  Conditions:
  - ResourcesAvailable = True   ← from Deployment
  - ContainerHealthy   = True   ← from Deployment (ReadyReplicas > 0)
  - Active             = True   ← from PodAutoscaler (non-zero scale)
  - Ready              = True   ← AND(ResourcesAvailable, ContainerHealthy)
          │
          │ Configuration reconciler calls SetLatestReadyRevisionName()
          │ when revision.IsReady() == true
          ▼
Configuration.Status
  LatestReadyRevisionName = "myapp-00002"
  LatestCreatedRevisionName = "myapp-00002"
  Conditions:
  - Ready = True                ← Created == Ready
          │
          │ PropagateConfigurationStatus()
          ▼
Service.Status
  LatestReadyRevisionName = "myapp-00002"  ← copied from Configuration
  Conditions:
  - ConfigurationsReady = True  ← from Configuration.Ready
  - RoutesReady         = True  ← from Route.Ready (see below)
  - Ready               = True  ← AND(ConfigurationsReady, RoutesReady)
  URL = "http://myapp.default.example.com" ← copied from Route

Separately, Route chain:
Ingress programmed by ingress controller
  → Ingress.Status.Ready = True
  → PropagateIngressStatus() → Route.IngressReady = True
  → Route.AllTrafficAssigned = True  (set by configureTraffic())
  → Route.CertificateProvisioned = True  (TLS cert ready or TLS disabled)
  → Route.Ready = True
  → PropagateRouteStatus() → Service.RoutesReady = True
```

### 3.8 Why Reconcilers Never Set Ready Directly

You will never find code like `rev.Status.Conditions = append(...)` or manual assignment to the `Ready` condition in any reconciler. The entire condition management is funneled through:

1. `lifecycle.Mark*True/False/Unknown()` — for specific sub-conditions
2. `lifecycle.Propagate*Status()` — to copy from a child resource
3. The `ConditionManager` — automatically maintains the `Ready` aggregate

This makes the system predictable: you can always trace why `Ready` has a particular value by checking the sub-conditions, and you can always find the reconciler code that set each sub-condition by searching for the corresponding `Mark*` call.

---

_Next: Section 4 — Controller Bootstrap_

---

## 4. Controller Bootstrap

### 4.1 The Single Binary

All core reconcilers run inside a **single process** — the `controller` binary. Its entry point is [cmd/controller/main.go](cmd/controller/main.go).

```go
// cmd/controller/main.go:56-66
var ctors = []injection.ControllerConstructor{
    configuration.NewController,
    labeler.NewController,
    revision.NewController,
    route.NewController,
    serverlessservice.NewController,
    service.NewController,
    gc.NewController,
    nscert.NewController,
    domainmapping.NewController,
}
```

Each entry is a constructor function with the signature:

```go
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl
```

They are all passed to `sharedmain.MainWithConfig`:

```go
// cmd/controller/main.go:92
sharedmain.MainWithConfig(ctx, "controller", cfg, ctors...)
```

`sharedmain.MainWithConfig` (from `knative.dev/pkg`) does the following for the entire process:
1. Parses kubeconfig and connects to the API server
2. Starts the **shared informer factory** — all informers in the process share a single cache
3. Calls each constructor in `ctors` to build every controller
4. Starts all the work-queue goroutines
5. Waits for the shared informer caches to sync (so listers have data before reconciliation begins)
6. Runs all controllers until the process receives a shutdown signal

The key insight is that the shared informer factory means **all controllers see the same in-memory cache**. If the Service controller and the Configuration controller both watch Configurations, they share one copy in memory, not two.

### 4.2 Informer Injection via Context

Every `NewController` function retrieves informers via a context-based injection system:

```go
// pkg/reconciler/service/controller.go:44-47
serviceInformer       := kserviceinformer.Get(ctx)
routeInformer         := routeinformer.Get(ctx)
configurationInformer := configurationinformer.Get(ctx)
revisionInformer      := revisioninformer.Get(ctx)
```

Each `Get(ctx)` call is generated code (in `pkg/client/injection/informers/`) that:
1. Looks up the informer by type from the shared factory stored in `ctx`
2. Returns the informer, starting it if it hasn't been started yet
3. Registers the informer's cache as a required sync target — the process won't begin reconciling until this cache is populated

This pattern means controllers declare their dependencies through imports and `Get(ctx)` calls. The framework handles deduplication: if two controllers both call `configurationinformer.Get(ctx)`, they get the same informer object.

### 4.3 The Three Event Handler Patterns

Every controller uses one of three patterns to hook informer events to the work queue.

#### Pattern 1: Direct Enqueue — watch the primary resource

When the resource that *this* controller owns changes, enqueue it directly:

```go
// pkg/reconciler/service/controller.go:63
serviceInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))
```

`impl.Enqueue` takes the object, extracts `namespace/name`, and puts it on the work queue. This fires on Add, Update, and Delete.

#### Pattern 2: FilterController + EnqueueControllerOf — bubble up from child to parent

When a child resource changes, find its owner and enqueue the **parent**:

```go
// pkg/reconciler/service/controller.go:65-70
handleControllerOf := cache.FilteringResourceEventHandler{
    FilterFunc: controller.FilterController(&v1.Service{}),
    Handler:    controller.HandleAll(impl.EnqueueControllerOf),
}
configurationInformer.Informer().AddEventHandler(handleControllerOf)
routeInformer.Informer().AddEventHandler(handleControllerOf)
```

- `controller.FilterController(&v1.Service{})` — inspects `OwnerReferences` on the event object. Only passes the event through if one of the owners has `Kind=Service`. Drops all events for Configurations/Routes not owned by a Service.
- `impl.EnqueueControllerOf` — walks `OwnerReferences`, finds the Service owner, and enqueues `namespace/service-name`.

This is the mechanism by which a change to a Configuration's status triggers the Service reconciler. The Configuration controller updates its status → Informer fires → FilterController passes the event → Service's queue gets the key.

The same pattern appears in the Configuration controller for Revisions:

```go
// pkg/reconciler/configuration/controller.go:58-61
revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
    FilterFunc: controller.FilterController(&v1.Configuration{}),
    Handler:    controller.HandleAll(impl.EnqueueControllerOf),
})
```

And in the Revision controller for Deployments and PodAutoscalers:

```go
// pkg/reconciler/revision/controller.go:136-141
handleMatchingControllers := cache.FilteringResourceEventHandler{
    FilterFunc: controller.FilterController(&v1.Revision{}),
    Handler:    controller.HandleAll(impl.EnqueueControllerOf),
}
deploymentInformer.Informer().AddEventHandler(handleMatchingControllers)
paInformer.Informer().AddEventHandler(handleMatchingControllers)
```

#### Pattern 3: Tracker — watch referenced resources (not owned)

The Route controller needs to reconcile when a Configuration or Revision it **references** (not owns) changes. These don't have OwnerReferences pointing at the Route, so `FilterController` won't work. Instead it uses a `tracker`:

```go
// pkg/reconciler/route/controller.go:113-131
configInformer.Informer().AddEventHandler(controller.HandleAll(
    controller.EnsureTypeMeta(
        c.tracker.OnChanged,
        v1.SchemeGroupVersion.WithKind("Configuration"),
    ),
))

revisionInformer.Informer().AddEventHandler(controller.HandleAll(
    controller.EnsureTypeMeta(
        c.tracker.OnChanged,
        v1.SchemeGroupVersion.WithKind("Revision"),
    ),
))
```

During `ReconcileKind`, the Route reconciler calls `c.tracker.TrackReference(ref, route)` to register "this Route is interested in this Configuration/Revision". Later, when that Configuration or Revision changes, `tracker.OnChanged` is called, which looks up all registered observers and enqueues them.

This enables the Route to react to `Configuration.Status.LatestReadyRevisionName` changing — a signal that new traffic targets are available.

### 4.4 ConfigMap Watchers and GlobalResync

Controllers need to react to runtime configuration changes (e.g. autoscaling parameters, domain templates). They use a `ConfigStore` that watches ConfigMaps:

```go
// pkg/reconciler/revision/controller.go:93-109
impl := revisionreconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
    configsToResync := []interface{}{
        &netcfg.Config{},
        &observability.Config{},
        &deployment.Config{},
        &apisconfig.Defaults{},
    }

    resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
        // When any of these ConfigMaps change, re-reconcile EVERY Revision
        impl.GlobalResync(revisionInformer.Informer())
    })

    configStore := config.NewStore(logger.Named("config-store"), resync)
    configStore.WatchConfigs(cmw)
    return controller.Options{ConfigStore: configStore}
})
```

`impl.GlobalResync(informer)` lists all objects from the informer and enqueues every one of them. This is used when a ConfigMap change affects how all resources of a type should be reconciled — for example, if the default container timeout changes, every Revision may need its Deployment updated.

During `ReconcileKind`, config is read from the store via:

```go
config.FromContext(ctx)  // returns the *config.Config snapshot for this reconcile call
```

The `ConfigStore` injects the current config into the context before each `ReconcileKind` call, so every reconcile always sees a consistent config snapshot.

### 4.5 The Revision Controller's Digest Resolution Queue

The Revision controller has a unique extra component: a background work queue dedicated to resolving container image tags to SHA digests:

```go
// pkg/reconciler/revision/controller.go:56
const digestResolutionWorkers = 100

// pkg/reconciler/revision/controller.go:123-131
digestResolveQueue := workqueue.NewTypedRateLimitingQueueWithConfig(
    workqueue.NewTypedMaxOfRateLimiter(
        newItemExponentialFailureRateLimiter(1*time.Second, 1000*time.Second),
        &workqueue.TypedBucketRateLimiter[any]{
            Limiter: rate.NewLimiter(rate.Limit(10), 100),
        },
    ),
    workqueue.TypedRateLimitingQueueConfig[any]{Name: "digests"},
)

resolver := newBackgroundResolver(
    logger,
    &digestResolver{client: kubeclient.Get(ctx), transport: transport, userAgent: userAgent},
    digestResolveQueue,
    impl.EnqueueKey,  // when digest is resolved, re-enqueue the Revision
)
resolver.Start(ctx.Done(), digestResolutionWorkers)
c.resolver = resolver
```

Why a separate queue? Resolving an image digest requires an outbound HTTP call to a container registry, which can be slow (100ms–2s). Running this in the main reconcile loop would block the worker goroutine for that duration, throttling overall throughput. The background resolver:
1. Accepts a Revision from `ReconcileKind`
2. Fires off 100 parallel HTTP requests to container registries
3. When a digest comes back, calls `impl.EnqueueKey` to re-enqueue that Revision
4. The Revision's next `ReconcileKind` call finds the digest in `rev.Status.ContainerStatuses` and proceeds to create the Deployment

### 4.6 Complete Event Handler Map

The table below shows every informer watched by each core controller and the handler pattern used:

```
┌─────────────────────┬─────────────────────────┬────────────────────────────┐
│   Controller        │   Informer Watched       │   Handler Pattern          │
├─────────────────────┼─────────────────────────┼────────────────────────────┤
│ Service             │ Service                  │ Direct Enqueue             │
│                     │ Configuration            │ FilterController → Owner   │
│                     │ Route                    │ FilterController → Owner   │
├─────────────────────┼─────────────────────────┼────────────────────────────┤
│ Configuration       │ Configuration            │ Direct Enqueue             │
│                     │ Revision                 │ FilterController → Owner   │
├─────────────────────┼─────────────────────────┼────────────────────────────┤
│ Revision            │ Revision                 │ Direct Enqueue             │
│                     │ Deployment               │ FilterController → Owner   │
│                     │ PodAutoscaler            │ FilterController → Owner   │
│                     │ Certificate              │ Tracker.OnChanged          │
├─────────────────────┼─────────────────────────┼────────────────────────────┤
│ Route               │ Route                    │ Direct Enqueue             │
│                     │ k8s Service (placeholder)│ FilterController → Owner   │
│                     │ Certificate              │ FilterController → Owner   │
│                     │ Ingress                  │ FilterController → Owner   │
│                     │ Configuration            │ Tracker.OnChanged          │
│                     │ Revision                 │ Tracker.OnChanged          │
└─────────────────────┴─────────────────────────┴────────────────────────────┘
```

### 4.7 Startup Sequence

The full startup sequence for the controller process:

```
1. main() parses flags, gets REST config from kubeconfig/in-cluster
2. Optionally registers cert-manager informers if TLS is configured
3. sharedmain.MainWithConfig() called with all 9 controller constructors
   │
   ├─ 4. Shared informer factory created and attached to context
   │
   ├─ 5. Each NewController() called in order:
   │      - Creates Reconciler struct with listers from informers
   │      - Calls generated reconciler.NewImpl() which creates work queue
   │        and worker goroutines
   │      - Registers all AddEventHandler calls on informers
   │      - Starts ConfigStore watching ConfigMaps
   │
   ├─ 6. Shared informer factory.Start() called
   │      - All informers begin Watch connections to API server
   │      - Events start flowing into informer caches
   │
   ├─ 7. WaitForCacheSync() — blocks until ALL informer caches have
   │      completed their initial list from the API server.
   │      No reconciliation happens yet.
   │
   └─ 8. Work queues are unblocked.
          Initial Add events from the list populate the queues.
          ReconcileKind begins being called for every existing object.
          The system converges to desired state.
```

Step 7 is critical: if reconciliation started before the cache was warm, a lister lookup for a resource that exists in etcd but hasn't been listed yet would return "not found" — causing a reconciler to incorrectly create a duplicate resource.

---

_Next: Section 5 — Service Reconciler_

---

## 5. Service Reconciler

**Files:**
- [pkg/reconciler/service/service.go](pkg/reconciler/service/service.go)
- [pkg/reconciler/service/controller.go](pkg/reconciler/service/controller.go)
- [pkg/reconciler/service/resources/configuration.go](pkg/reconciler/service/resources/configuration.go)
- [pkg/reconciler/service/resources/route.go](pkg/reconciler/service/resources/route.go)
- [pkg/reconciler/service/resources/names/names.go](pkg/reconciler/service/resources/names/names.go)

### 5.1 Responsibility

The Service reconciler is the **topmost orchestrator**. Its sole job is:
1. Ensure a `Configuration` exists that matches `service.Spec.ConfigurationSpec`
2. Ensure a `Route` exists that matches `service.Spec.RouteSpec`
3. Reflect the status of both children back onto the Service's own status

It never touches Revisions, Deployments, Ingresses, or Pods — those belong to deeper controllers.

### 5.2 The Reconciler Struct

```go
// pkg/reconciler/service/service.go:46-53
type Reconciler struct {
    client clientset.Interface  // to create/update Configuration and Route

    // listers read from the local informer cache (no API calls)
    configurationLister listers.ConfigurationLister
    revisionLister      listers.RevisionLister
    routeLister         listers.RouteLister
}
```

The Service reconciler only holds listers for resources it needs to read and a client for resources it needs to write. It has **no lister for Services itself** — the generated reconciler framework fetches the Service for it before calling `ReconcileKind`.

### 5.3 ReconcileKind — Annotated Full Walk-through

```go
// pkg/reconciler/service/service.go:72-127
func (c *Reconciler) ReconcileKind(ctx context.Context, service *v1.Service) pkgreconciler.Event {
    ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
    defer cancel()

    logger := logging.FromContext(ctx)

    // ── STEP 1: Reconcile the Configuration ──────────────────────────────────
    config, err := c.Config(ctx, service)
    if err != nil {
        return err
    }
```

`c.Config()` is the first gate. It either creates or updates the Configuration owned by this Service. If the Configuration already exists but is owned by someone else, this returns an error and sets `ServiceConditionConfigurationsReady=False`. If creation fails (API server error), it is returned as a transient error and will be retried.

```go
    // ── STEP 2: Generation guard ──────────────────────────────────────────────
    if config.Generation != config.Status.ObservedGeneration {
        // The Configuration controller hasn't processed our latest spec yet.
        // Its conditions reflect the OLD spec — don't trust them.
        service.Status.MarkConfigurationNotReconciled()

        // Special case: if user specified an explicit revision name (BYO-Name),
        // we MUST wait before programming the Route. Otherwise the Route could
        // send traffic to the wrong revision.
        if config.Spec.GetTemplate().Name != "" {
            return nil  // stop here, retry when Configuration reconciles
        }
    } else {
        logger.Debugf("Configuration Conditions = %#v", config.Status.Conditions)
        // Configuration is up to date — copy its status up to Service
        service.Status.PropagateConfigurationStatus(&config.Status)
    }
```

The generation guard (covered in Section 3.4) is critical here. If the Service's spec just changed, the Configuration was updated but the Configuration controller may not have run yet. Propagating stale conditions would show `Ready=True` with outdated revision names. `MarkConfigurationNotReconciled()` sets `ConfigurationsReady=Unknown` with reason `"OutOfDate"`.

The BYO-Name early return prevents a race: if you name your revision explicitly (e.g. `template.metadata.name: myapp-v2`), the Route must not be programmed until that exact revision name exists and is confirmed owned by the Configuration.

```go
    // ── STEP 3: Revision name availability check ──────────────────────────────
    if err := CheckNameAvailability(config, c.revisionLister); err != nil &&
        !apierrs.IsNotFound(err) {
        service.Status.MarkRevisionNameTaken(config.Spec.GetTemplate().Name)
        return nil
    }
```

`CheckNameAvailability` only matters when the template has an explicit `Name`. It looks up the named Revision and checks:
- Does it exist? If not found → fine, continue (the Configuration reconciler will create it).
- Does it exist but belong to a different Configuration? → Conflict. Mark `RoutesReady=False` with `RevisionNameTaken` and stop. This prevents accidentally routing traffic to a pre-existing Revision that happened to have the same name.
- Does it exist, owned by our Configuration, with matching spec? → Fine, continue.

```go
    // ── STEP 4: Reconcile the Route ───────────────────────────────────────────
    route, err := c.route(ctx, service)
    if err != nil {
        return err
    }

    // ── STEP 5: Generation guard + status propagation for Route ──────────────
    ss := &service.Status
    if route.Generation != route.Status.ObservedGeneration {
        ss.MarkRouteNotReconciled()
    } else {
        ss.PropagateRouteStatus(&route.Status)
    }

    // ── STEP 6: Final traffic correctness check ───────────────────────────────
    c.checkRoutesNotReady(config, logger, route, service)
    return nil
}
```

Steps 4 and 5 mirror steps 1 and 2 for the Route side. `PropagateRouteStatus` copies the Route's URL, Address, resolved Traffic table, and `RoutesReady` condition up to the Service.

Step 6 is the final consistency guard — covered in detail in 5.6.

### 5.4 c.Config() — Create or Reconcile the Configuration

```go
// pkg/reconciler/service/service.go:129-150
func (c *Reconciler) Config(ctx context.Context, service *v1.Service) (*v1.Configuration, error) {
    recorder := controller.GetEventRecorder(ctx)
    configName := resourcenames.Configuration(service)  // returns service.Name
    config, err := c.configurationLister.Configurations(service.Namespace).Get(configName)

    if apierrs.IsNotFound(err) {
        // Doesn't exist yet — create it
        config, err = c.createConfiguration(ctx, service)
        if err != nil {
            recorder.Eventf(service, corev1.EventTypeWarning, "CreationFailed",
                "Failed to create Configuration %q: %v", configName, err)
            return nil, fmt.Errorf("failed to create Configuration: %w", err)
        }
        recorder.Eventf(service, corev1.EventTypeNormal, "Created",
            "Created Configuration %q", configName)

    } else if err != nil {
        return nil, fmt.Errorf("failed to get Configuration: %w", err)

    } else if !metav1.IsControlledBy(config, service) {
        // Exists but belongs to someone else — surface ownership error
        service.Status.MarkConfigurationNotOwned(configName)
        return nil, fmt.Errorf("service: %q does not own configuration: %q",
            service.Name, configName)

    } else if config, err = c.reconcileConfiguration(ctx, service, config); err != nil {
        return nil, fmt.Errorf("failed to reconcile Configuration: %w", err)
    }

    return config, nil
}
```

This is the idiomatic **get-or-create-or-update** pattern used throughout Knative:

```
lister.Get(name)
  → NotFound   → Create
  → Error      → return error (retry)
  → Found, not owned by us → surface ownership error
  → Found, owned by us → reconcile (update if spec drifted)
```

The ownership check `metav1.IsControlledBy(config, service)` inspects the Configuration's `OwnerReferences` to confirm this Service is the controller owner. This prevents the Service from taking over a Configuration created by another Service with the same name.

### 5.5 reconcileConfiguration() — Semantic Diff and Update

```go
// pkg/reconciler/service/service.go:222-246
func (c *Reconciler) reconcileConfiguration(ctx context.Context, service *v1.Service,
    config *v1.Configuration) (*v1.Configuration, error) {

    existing := config.DeepCopy()
    // Apply current defaults so a diff caused only by new default values
    // doesn't trigger a spurious update
    existing.SetDefaults(ctx)

    desiredConfig := resources.MakeConfigurationFromExisting(service, existing)
    equals, err := configSemanticEquals(ctx, desiredConfig, existing)
    if err != nil {
        return nil, err
    }
    if equals {
        return config, nil  // nothing changed — no API call
    }

    logger.Warnf("Service-delegated Configuration %q diff found. Clobbering.", existing.Name)

    // Only replace the mutable parts — preserve ObjectMeta fields we don't own
    existing.Spec        = desiredConfig.Spec
    existing.Labels      = desiredConfig.Labels
    existing.Annotations = desiredConfig.Annotations
    return c.client.ServingV1().Configurations(service.Namespace).Update(
        ctx, existing, metav1.UpdateOptions{})
}
```

`configSemanticEquals` compares `Spec`, `Labels`, and `Annotations` using `equality.Semantic.DeepEqual`. This is a **semantic** diff, not a byte diff — Kubernetes can round-trip JSON in ways that change bytes but not meaning (e.g. reordering map keys).

The `SetDefaults(ctx)` call before the diff is important: after a Knative upgrade, newly defaulted fields might appear in the desired spec but not in the stored object (which was created before the upgrade). Without this step, every existing Configuration would appear to have drifted and receive a spurious update on restart.

### 5.6 checkRoutesNotReady() — The Traffic Consistency Guard

Even when `PropagateRouteStatus` sets `RoutesReady=True`, there is one additional check:

```go
// pkg/reconciler/service/service.go:175-200
func (c *Reconciler) checkRoutesNotReady(config *v1.Configuration,
    logger *zap.SugaredLogger, route *v1.Route, service *v1.Service) {

    rc := service.Status.GetCondition(v1.ServiceConditionRoutesReady)
    if rc == nil || rc.Status != corev1.ConditionTrue {
        return  // already not ready — nothing to downgrade
    }

    // Spec traffic count must match status traffic count
    if len(route.Spec.Traffic) != len(route.Status.Traffic) {
        service.Status.MarkRouteNotYetReady()
        return
    }

    want := route.Spec.DeepCopy().Traffic
    got  := route.Status.DeepCopy().Traffic

    // Replace ConfigurationName targets with their current LatestReadyRevisionName
    // so we compare apples-to-apples with the status (which always uses RevisionName)
    for idx := range want {
        if want[idx].ConfigurationName == config.Name {
            want[idx].RevisionName        = config.Status.LatestReadyRevisionName
            want[idx].ConfigurationName   = ""
        }
    }

    ignoreFields := cmpopts.IgnoreFields(v1.TrafficTarget{}, "URL", "LatestRevision")
    if diff, err := kmp.SafeDiff(got, want, ignoreFields); err != nil || diff != "" {
        logger.Errorf("Route %s is not yet what we want: %s", route.Name, diff)
        service.Status.MarkRouteNotYetReady()
    }
}
```

Why is this needed if `RoutesReady=True` was already propagated?

Consider this timing scenario:
1. User updates Service to deploy `image:v2`
2. Configuration creates Revision `myapp-00002` — `LatestReadyRevisionName` becomes `"myapp-00002"`
3. Route is updated to send 100% traffic to `myapp-00002`
4. The Ingress layer programs the load balancer
5. Route sets `RoutesReady=True` and reflects `myapp-00002` in `Status.Traffic`
6. User updates Service *again* to deploy `image:v3`
7. Configuration creates Revision `myapp-00003` — `LatestReadyRevisionName` becomes `"myapp-00003"`
8. Route reconciler hasn't run yet
9. Service reconciler runs: `PropagateRouteStatus` sets `RoutesReady=True` (Route is still `Ready=True` from step 5)
10. But `Route.Status.Traffic` still shows `myapp-00002`, while `config.Status.LatestReadyRevisionName` is now `myapp-00003`

Without `checkRoutesNotReady`, `Service.Status` would misleadingly show `Ready=True` while traffic is still flowing to the old revision. The check catches this gap by comparing what the Route's traffic table *should* contain (the current `LatestReadyRevisionName`) against what it *actually* contains, and downgrades to `RoutesReady=Unknown` with reason `"TrafficNotMigrated"` until they match.

### 5.7 Service Reconciler State Machine

```
Service created / updated
          │
          ▼
    c.Config(ctx, service)
          │
    ┌─────┴──────────────────────────────────────────┐
    │  Configuration                                  │
    │  NotFound → create → emit "Created" event       │
    │  Found, wrong owner → MarkConfigurationNotOwned │
    │  Found, owned → reconcileConfiguration()        │
    │    (semantic diff → update if needed)            │
    └─────┬───────────────────────────────────────────┘
          │
          ▼
    Generation guard on Configuration
    ┌─────┴─────────────────────────────────────────────┐
    │ config.Gen != config.Status.ObservedGen?           │
    │   YES → MarkConfigurationNotReconciled             │
    │         BYO-Name? → return nil (wait)              │
    │   NO  → PropagateConfigurationStatus()             │
    └─────┬─────────────────────────────────────────────┘
          │
          ▼
    CheckNameAvailability (BYO-Name only)
    ┌─────┴─────────────────────────────────────────────┐
    │ Name taken by wrong owner? → MarkRevisionNameTaken │
    │ Name free or owned by us  → continue              │
    └─────┬─────────────────────────────────────────────┘
          │
          ▼
    c.route(ctx, service)
          │
    ┌─────┴──────────────────────────────────────────┐
    │  Route                                          │
    │  NotFound → create → emit "Created" event       │
    │  Found, wrong owner → MarkRouteNotOwned         │
    │  Found, owned → reconcileRoute()                │
    │    (semantic diff → update if needed)            │
    └─────┬───────────────────────────────────────────┘
          │
          ▼
    Generation guard on Route
    ┌─────┴──────────────────────────────────────────────┐
    │ route.Gen != route.Status.ObservedGen?              │
    │   YES → MarkRouteNotReconciled                      │
    │   NO  → PropagateRouteStatus()                      │
    └─────┬──────────────────────────────────────────────┘
          │
          ▼
    checkRoutesNotReady()
    ┌─────┴──────────────────────────────────────────────┐
    │ Route.Status.Traffic matches expected? (by revision)│
    │   NO  → MarkRouteNotYetReady                        │
    │   YES → leave RoutesReady=True                      │
    └─────┬──────────────────────────────────────────────┘
          │
          ▼
    return nil
    Generated framework calls UpdateStatus() if status changed
```

### 5.8 What the Service Reconciler Does NOT Do

Understanding the boundaries is as important as understanding what it does:

| Concern | Owned by |
|---------|----------|
| Creating Revisions | Configuration reconciler |
| Creating Deployments | Revision reconciler |
| Creating PodAutoscalers | Revision reconciler |
| Programming Ingress/load balancer | Route reconciler |
| Scale-to-zero / scale-from-zero | Autoscaler + Activator |
| TLS certificates | Route reconciler + Certificate reconciler |
| Garbage collecting old Revisions | GC reconciler |

The Service reconciler is intentionally thin. It creates two children and reflects their status. Everything else is delegated.

---

_Next: Section 6 — Configuration Reconciler_

---

## 6. Configuration Reconciler

**Files:**
- [pkg/reconciler/configuration/configuration.go](pkg/reconciler/configuration/configuration.go)
- [pkg/reconciler/configuration/controller.go](pkg/reconciler/configuration/controller.go)
- [pkg/reconciler/configuration/resources/revision.go](pkg/reconciler/configuration/resources/revision.go)

### 6.1 Responsibility

The Configuration reconciler does exactly two things:
1. **Stamp out a new Revision** whenever `Configuration.Spec.Template` changes (i.e. `Configuration.Generation` increments)
2. **Track which Revision is ready** and update `LatestReadyRevisionName` when a newer one becomes ready

It never modifies Deployments, Routes, or Services. It only creates Revisions and updates its own status.

### 6.2 The Reconciler Struct

```go
// pkg/reconciler/configuration/configuration.go:46-53
type Reconciler struct {
    client         clientset.Interface  // to create Revisions

    revisionLister listers.RevisionLister  // read-only, from informer cache

    clock          clock.PassiveClock  // injectable for testing determinism
}
```

The `clock` field is the only unusual thing here. It is injected (not `time.Now()` directly) so tests can set a fixed timestamp. Timestamps are embedded in Revision labels (the `RoutingStateModified` annotation), so determinism matters for test assertions.

### 6.3 ReconcileKind — Annotated Full Walk-through

```go
// pkg/reconciler/configuration/configuration.go:59-136
func (c *Reconciler) ReconcileKind(ctx context.Context, config *v1.Configuration) pkgreconciler.Event {
    ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
    defer cancel()

    logger   := logging.FromContext(ctx)
    recorder := controller.GetEventRecorder(ctx)
```

#### Step 1 — Find or Create the Latest Revision

```go
    lcr, isBYOName, err := c.latestCreatedRevision(ctx, config)

    if errors.IsNotFound(err) {
        // No revision exists for this generation yet — create one
        lcr, err = c.createRevision(ctx, config)
        if errors.IsAlreadyExists(err) {
            // Race: another reconcile loop beat us to it.
            // Fail and retry — next run will find it via latestCreatedRevision.
            return fmt.Errorf("failed to create Revision: %w", err)
        } else if err != nil {
            recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed",
                "Failed to create Revision: %v", err)
            config.Status.MarkRevisionCreationFailed(err.Error())
            return fmt.Errorf("failed to create Revision: %w", err)
        }
    } else if errors.IsAlreadyExists(err) {
        // latestCreatedRevision found a naming conflict — another Configuration
        // owns a Revision with the name we wanted.
        config.Status.MarkRevisionCreationFailed(err.Error())
        return nil  // terminal — user must fix the name conflict
    } else if err != nil {
        return fmt.Errorf("failed to get Revision: %w", err)
    }
```

Two different `AlreadyExists` paths:
- From `createRevision`: a concurrent reconcile won the race → retry (transient)
- From `latestCreatedRevision`: the desired revision name is taken by a different owner → surface error (terminal until user fixes)

#### Step 2 — Record the Latest Created Revision

```go
    revName := lcr.Name
    // Always record which revision we consider "latest created"
    config.Status.SetLatestCreatedRevisionName(revName)
```

`SetLatestCreatedRevisionName` (from Section 3.5) sets `LatestCreatedRevisionName` and marks `ConfigurationConditionReady=Unknown` if it doesn't match `LatestReadyRevisionName` yet.

#### Step 3 — Inspect the Revision's Ready Condition

```go
    rc := lcr.Status.GetCondition(v1.RevisionConditionReady)
    switch {
    case rc.IsUnknown():
        // Still deploying — nothing to do, wait for Revision reconciler
        logger.Infof("Revision %q of configuration is not ready", revName)

    case rc.IsTrue():
        // Revision just became ready for the first time ever
        logger.Infof("Revision %q of configuration is ready", revName)
        if config.Status.LatestReadyRevisionName == "" {
            recorder.Event(config, corev1.EventTypeNormal, "ConfigurationReady",
                "Configuration becomes ready")
        }

    case rc.IsFalse():
        // Revision failed — mark Configuration as failed too
        logger.Infof("Revision %q of configuration has failed: Reason=%s Message=%q",
            revName, rc.Reason, rc.Message)
        beforeReady := config.Status.GetCondition(v1.ConfigurationConditionReady)
        config.Status.MarkLatestCreatedFailed(lcr.Name, rc.GetMessage())

        // Emit a warning event only if the condition actually changed
        if !equality.Semantic.DeepEqual(beforeReady,
            config.Status.GetCondition(v1.ConfigurationConditionReady)) {
            if lcr.Name == config.Status.LatestReadyRevisionName {
                recorder.Eventf(config, corev1.EventTypeWarning, "LatestReadyFailed",
                    "Latest ready revision %q has failed", lcr.Name)
            } else {
                recorder.Eventf(config, corev1.EventTypeWarning, "LatestCreatedFailed",
                    "Latest created revision %q has failed", lcr.Name)
            }
        }
    }
```

The event de-duplication (`DeepEqual(beforeReady, after)`) prevents flooding the event stream. Without it, every reconcile loop iteration while a Revision is failing would emit a new warning event.

#### Step 4 — Find and Promote the Latest Ready Revision

```go
    if err = c.findAndSetLatestReadyRevision(ctx, config, lcr, isBYOName); err != nil {
        return fmt.Errorf("failed to find and set latest ready revision: %w", err)
    }
    return nil
}
```

Even when `lcr` (the latest created) is not ready, there might be an older revision that just became ready. This step searches for it and updates `LatestReadyRevisionName` accordingly.

### 6.4 latestCreatedRevision() — Two Lookup Paths

```go
// pkg/reconciler/configuration/configuration.go:277-297
func (c *Reconciler) latestCreatedRevision(ctx context.Context,
    config *v1.Configuration) (*v1.Revision, bool, error) {

    // Path 1: BYO-Name — user specified template.metadata.name
    if rev, err := CheckNameAvailability(ctx, config, c.revisionLister); rev != nil || err != nil {
        return rev, true, err  // isBYOName=true
    }

    // Path 2: Auto-named — look up by the generation label
    generationKey := serving.ConfigurationGenerationLabelKey
    list, err := lister.List(labels.SelectorFromSet(labels.Set{
        generationKey:                 resources.RevisionLabelValueForKey(generationKey, config),
        serving.ConfigurationLabelKey: config.Name,
    }))

    if err == nil && len(list) > 0 {
        return list[0], false, nil  // isBYOName=false
    }

    return nil, false, errors.NewNotFound(v1.Resource("revisions"), "revision for "+config.Name)
}
```

**Path 1 — BYO-Name:** When the user sets `spec.template.metadata.name: myapp-v2`, `CheckNameAvailability` looks up that exact name. It returns:
- `rev, nil` — found and owned by this Configuration at the right generation → proceed
- `nil, AlreadyExists` — found but owned by another Configuration → surface conflict
- `nil, NotFound` — doesn't exist yet → `createRevision` will be called

**Path 2 — Auto-named:** Query by two labels:
- `serving.knative.dev/configuration: myapp` — only revisions belonging to this Configuration
- `serving.knative.dev/configurationGeneration: "5"` — only revisions for this exact generation

This is an O(1) lookup by label index (the informer cache maintains label indexes). The generation label is set by `MakeRevision` at creation time and never changes.

### 6.5 createRevision() and MakeRevision()

```go
// pkg/reconciler/configuration/configuration.go:299-311
func (c *Reconciler) createRevision(ctx context.Context, config *v1.Configuration) (*v1.Revision, error) {
    logger := logging.FromContext(ctx)

    rev := resources.MakeRevision(ctx, config, c.clock.Now())
    created, err := c.client.ServingV1().Revisions(config.Namespace).Create(ctx, rev, metav1.CreateOptions{})
    if err != nil {
        return nil, err
    }
    controller.GetEventRecorder(ctx).Eventf(config, corev1.EventTypeNormal,
        "Created", "Created Revision %q", created.Name)
    logger.Infof("Created Revision: %#v", created)
    return created, nil
}
```

`MakeRevision` is where all the interesting assembly happens:

```go
// pkg/reconciler/configuration/resources/revision.go:32-55
func MakeRevision(ctx context.Context, configuration *v1.Configuration, tm time.Time) *v1.Revision {
    // Start from what the user put in spec.template — this carries their
    // container image, env vars, resource requests, etc.
    rev := &v1.Revision{
        ObjectMeta: configuration.Spec.GetTemplate().ObjectMeta,
        Spec:       configuration.Spec.GetTemplate().Spec,
    }

    rev.Namespace = configuration.Namespace

    // Auto-generate a name if the user didn't supply one:
    //   "myapp" + generation 3 → "myapp-00003"
    if rev.Name == "" {
        rev.Name = kmeta.ChildName(configuration.Name,
            fmt.Sprintf("-%05d", configuration.Generation))
    }

    // Mark the revision as "pending" — it has not yet been routed by the labeler
    rev.SetRoutingState(v1.RoutingStatePending, tm)

    updateRevisionLabels(rev, configuration)
    updateRevisionAnnotations(rev, configuration, tm)

    // Set OwnerReference so deletes cascade: deleting Configuration deletes Revision
    rev.OwnerReferences = append(rev.OwnerReferences,
        *kmeta.NewControllerRef(configuration))

    return rev
}
```

#### Revision Naming

The auto-name format is `kmeta.ChildName(configName, "-00003")`:
- `kmeta.ChildName` truncates if `configName` is long, then appends the suffix
- The zero-padded generation (`%05d`) means lexicographic sort order matches chronological order: `myapp-00001 < myapp-00002 < myapp-00003`

#### Labels Applied to Every Revision

```go
// pkg/reconciler/configuration/resources/revision.go:57-77
func updateRevisionLabels(rev, config metav1.Object) {
    for _, key := range []string{
        serving.ConfigurationLabelKey,           // "serving.knative.dev/configuration" = "myapp"
        serving.ServiceLabelKey,                 // "serving.knative.dev/service" = "myapp" (if owned by a Service)
        serving.ConfigurationGenerationLabelKey, // "serving.knative.dev/configurationGeneration" = "3"
        serving.ConfigurationUIDLabelKey,        // "serving.knative.dev/configurationUID" = "<uuid>"
        serving.ServiceUIDLabelKey,              // "serving.knative.dev/serviceUID" = "<uuid>" (if applicable)
    } {
        if value := RevisionLabelValueForKey(key, config); value != "" {
            labels[key] = value
        }
    }
}
```

These labels are what make the informer cache queries efficient — they are the indexes used in `latestCreatedRevision()` and in `getSortedCreatedRevisions()`.

#### RoutingState Label

```go
// revision.go:45-46
rev.SetRoutingState(v1.RoutingStatePending, tm)
```

Every new Revision starts life as `RoutingStatePending`. This label (`serving.knative.dev/routingState`) is watched by the **Labeler controller** — once the Route references this Revision, the Labeler transitions it to `RoutingStateActive`. When no Route references it (after a traffic shift away), it becomes `RoutingStateReserve`. The GC controller uses this state to decide which Revisions to delete.

### 6.6 findAndSetLatestReadyRevision()

```go
// pkg/reconciler/configuration/configuration.go:139-157
func (c *Reconciler) findAndSetLatestReadyRevision(ctx context.Context,
    config *v1.Configuration, latestCreated *v1.Revision, isBYOName bool) error {

    sortedRevisions, err := c.getSortedCreatedRevisions(ctx, config, latestCreated, isBYOName)
    if err != nil {
        return err
    }

    for _, rev := range sortedRevisions {
        if rev.IsReady() {
            old, new := config.Status.LatestReadyRevisionName, rev.Name
            config.Status.SetLatestReadyRevisionName(rev.Name)
            if old != new {
                controller.GetEventRecorder(ctx).Eventf(config,
                    corev1.EventTypeNormal, "LatestReadyUpdate",
                    "LatestReadyRevisionName updated to %q", rev.Name)
            }
            return nil  // found the newest ready revision — stop
        }
    }
    return nil  // no ready revision found — keep existing LatestReadyRevisionName
}
```

The list is sorted newest-first. The loop stops at the **first** ready revision it finds — that is, the newest ready one. This is important: if revisions 1, 2, 3 exist and revision 3 is the newest but is still deploying, revision 2 (which is ready) remains `LatestReadyRevisionName` until revision 3 becomes ready.

### 6.7 getSortedCreatedRevisions() — The Generation Range Query

This is the most algorithmically interesting part of the Configuration reconciler:

```go
// pkg/reconciler/configuration/configuration.go:161-226
func (c *Reconciler) getSortedCreatedRevisions(ctx context.Context,
    config *v1.Configuration, latestCreated *v1.Revision, isBYOName bool) ([]*v1.Revision, error) {

    lister := c.revisionLister.Revisions(config.Namespace)

    // Base selector: all Revisions belonging to this Configuration
    configSelector := labels.SelectorFromSet(labels.Set{
        serving.ConfigurationLabelKey: config.Name,
    })

    // Optimisation: only query the generation range we care about.
    // We only need revisions between the current LatestReady and the current generation.
    // Revisions older than LatestReady can never become the new LatestReady.
    if config.Status.LatestReadyRevisionName != "" {
        lrr, err := lister.Get(config.Status.LatestReadyRevisionName)
        if err != nil {
            logger.Errorf("Error getting latest ready revision %q: %v",
                config.Status.LatestReadyRevisionName, err)
            // Continue without the optimisation if we can't find it
        } else {
            start, _ := configGeneration(lrr)

            // Build a list of generation numbers: [start, start+1, ..., config.Generation]
            var generations []string
            for i := start; i <= config.Generation; i++ {
                generations = append(generations, strconv.FormatInt(i, 10))
            }
            // Also include the BYO-Name revision's generation if applicable
            if isBYOName {
                lcrGen, err := configGeneration(latestCreated)
                if err == nil {
                    generations = append(generations, strconv.FormatInt(lcrGen, 10))
                }
            }

            // Add "In" requirement: configurationGeneration in [start..current]
            inReq, err := labels.NewRequirement(
                serving.ConfigurationGenerationLabelKey,
                selection.In,
                generations,
            )
            if err == nil {
                configSelector = configSelector.Add(*inReq)
            }
        }
    }

    list, err := lister.List(configSelector)
    if err != nil {
        return nil, err
    }

    // Sort by configurationGeneration descending (newest first)
    // BYO-Name revision always sorts first regardless of generation
    if len(list) > 1 {
        sort.Slice(list, func(i, j int) bool {
            if config.Spec.Template.Name == list[i].Name { return true }
            if config.Spec.Template.Name == list[j].Name { return false }
            intI, errI := configGeneration(list[i])
            intJ, errJ := configGeneration(list[j])
            if errI != nil || errJ != nil { return true }
            return intI > intJ  // descending
        })
    }
    return list, nil
}
```

**Why the generation range query?**

Without it, a Configuration with 1000 old Revisions (many past deployments) would list all 1000 on every reconcile just to find the newest ready one. The range constraint `[latestReadyGeneration .. currentGeneration]` limits the query to only the Revisions that could possibly *become* the new `LatestReadyRevisionName`. Any revision older than the current `LatestReady` is irrelevant — it was already considered and superseded.

For example, if `LatestReady=myapp-00098` and the current generation is `100`:
- Only Revisions with `configurationGeneration` in `{98, 99, 100}` are queried
- The list is at most 3 items regardless of total history

### 6.8 Configuration Reconciler State Machine

```
Configuration created (Generation=1) or spec updated (Generation increments)
          │
          ▼
    latestCreatedRevision(config)
    ┌─────┴──────────────────────────────────────────────────────────┐
    │                                                                 │
    │  BYO-Name path:              Auto-name path:                   │
    │  CheckNameAvailability()     List by labels:                   │
    │  look up template.Name        configurationLabelKey=myapp      │
    │                               configurationGenerationLabelKey=N │
    └─────┬───────────────────────┬─────────────────────────────────┘
          │                       │
     Found, owned            Not Found
     by this config          (NotFound error)
          │                       │
          ▼                       ▼
   use existing rev         createRevision(config)
                            └── MakeRevision():
                                  - Name = "myapp-00003" (auto) or template.Name
                                  - Spec  = template.Spec (PodSpec + concurrency)
                                  - Labels: configurationLabelKey,
                                            generationLabelKey,
                                            serviceLabelKey (if set)
                                  - RoutingState = "pending"
                                  - OwnerRef → Configuration
          │
          ▼
    config.Status.SetLatestCreatedRevisionName(rev.Name)
    (if Created != Ready → ConfigurationReady=Unknown)
          │
          ▼
    Check lcr.Status.GetCondition(RevisionConditionReady)
    ┌─────┴───────────────────────────────────────────────┐
    │ Unknown → log "not ready yet", continue             │
    │ True    → log "ready", emit ConfigurationReady      │
    │           event if first time                       │
    │ False   → MarkLatestCreatedFailed()                 │
    │           emit warning event (once, deduped)        │
    └─────┬───────────────────────────────────────────────┘
          │
          ▼
    findAndSetLatestReadyRevision()
       getSortedCreatedRevisions()
         List revisions in generation range [LRR.gen .. config.gen]
         Sort descending by generation
       Iterate sorted list:
         First rev where rev.IsReady() == true
           → config.Status.SetLatestReadyRevisionName(rev.Name)
           → emit LatestReadyUpdate event
           → stop
       No ready rev found → keep existing LatestReadyRevisionName
          │
          ▼
    return nil
    Framework calls UpdateStatus()
```

### 6.9 The Immutability Guarantee

A Revision's `Spec` is immutable — the webhook rejects any attempt to update it. This is enforced in [pkg/apis/serving/v1/revision_validation.go](pkg/apis/serving/v1/revision_validation.go).

The Configuration reconciler relies on this guarantee for correctness. `latestCreatedRevision` finds a Revision by generation label and then uses it as-is, without verifying its Spec matches the template. It trusts that because Revisions are immutable, a Revision with `configurationGeneration=5` will always have the exact spec the Configuration had at generation 5.

If Revisions were mutable, an attacker or buggy controller could change a Revision's spec after creation, and the Route would silently send traffic to modified pods.

### 6.10 What Happens When the Latest Ready Revision Is Deleted

If a user deletes `LatestReadyRevisionName` manually:

1. `lister.Get(config.Status.LatestReadyRevisionName)` in `getSortedCreatedRevisions` returns an error
2. The code logs it and **continues without the generation range optimisation** — it falls back to listing all Revisions by `ConfigurationLabelKey` only
3. The sorted list will not contain the deleted revision
4. The next newest ready revision (if any) is promoted to `LatestReadyRevisionName`
5. If no ready revisions remain, `LatestReadyRevisionName` stays as the deleted name until either a new revision becomes ready or the system reconciles further
6. `MarkLatestReadyDeleted()` is triggered, setting `ConfigurationConditionReady=False`

---

_Next: Section 7 — Revision Reconciler_

---

## 7. Revision Reconciler

**Files:**
- [pkg/reconciler/revision/revision.go](pkg/reconciler/revision/revision.go)
- [pkg/reconciler/revision/reconcile_resources.go](pkg/reconciler/revision/reconcile_resources.go)
- [pkg/reconciler/revision/background.go](pkg/reconciler/revision/background.go)
- [pkg/reconciler/revision/controller.go](pkg/reconciler/revision/controller.go)
- [pkg/reconciler/revision/resources/deploy.go](pkg/reconciler/revision/resources/deploy.go)
- [pkg/reconciler/revision/resources/pa.go](pkg/reconciler/revision/resources/pa.go)

### 7.1 Responsibility

The Revision reconciler is the most complex of the four core reconcilers. It owns:
1. **Image digest resolution** — resolving `myapp:latest` → `myapp@sha256:abc123` before creating anything
2. **Deployment creation/update** — the Kubernetes Deployment that runs user pods with the queue-proxy sidecar injected
3. **PodAutoscaler creation/update** — the KPA/HPA object that drives scaling decisions
4. **Image cache creation** — pre-pull optimization via `caching.knative.dev/Image`
5. **TLS certificate** — per-namespace certificate for the queue-proxy when system-internal TLS is on

It never creates Revisions (that is the Configuration reconciler's job), never touches Routes or Ingresses, and never makes traffic routing decisions.

### 7.2 The Reconciler Struct

```go
// pkg/reconciler/revision/revision.go:58-72
type Reconciler struct {
    kubeclient       kubernetes.Interface        // for creating Deployments, reading Pods
    client           clientset.Interface         // for creating PodAutoscalers
    networkingclient networkingclientset.Interface // for creating Knative Certificates
    cachingclient    cachingclientset.Interface   // for creating Image cache objects

    // listers — all reads go through the informer cache
    podAutoscalerLister palisters.PodAutoscalerLister
    imageLister         cachinglisters.ImageLister
    deploymentLister    appsv1listers.DeploymentLister
    certificateLister   networkinglisters.CertificateLister

    tracker  tracker.Interface   // for tracking non-owned references (Certificates)
    resolver resolver            // background image digest resolver
}
```

The `resolver` field is the only interface that is not a standard Kubernetes lister or client. It is the `backgroundResolver` (explained in 7.4), injected so tests can substitute a synchronous fake.

### 7.3 ReconcileKind — Four Sequential Phases

```go
// pkg/reconciler/revision/revision.go:122-175
func (c *Reconciler) ReconcileKind(ctx context.Context, rev *v1.Revision) pkgreconciler.Event {
    ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
    defer cancel()

    readyBeforeReconcile := rev.IsReady()
    c.updateRevisionLoggingURL(ctx, rev)
```

`readyBeforeReconcile` captures the pre-reconcile state so the reconciler can emit an event exactly once when a revision transitions into or out of ready.

`updateRevisionLoggingURL` fills in `rev.Status.LogURL` from the observability config's `LoggingURLTemplate`, substituting `${REVISION_UID}`. This is a pure status update — no child resources involved.

#### Phase 1 — Image Digest Resolution (Gate)

```go
    reconciled, err := c.reconcileDigest(ctx, rev)
    if err != nil {
        return err
    }
    if !reconciled {
        // Digest not resolved yet. Mark status and exit.
        // The background resolver will re-enqueue this Revision when done.
        rev.Status.MarkResourcesAvailableUnknown(v1.ReasonResolvingDigests, "")
        return nil
    }
```

This is a **hard gate**. If the image digests are not resolved, the reconciler sets `ResourcesAvailable=Unknown` with reason `ResolvingDigests` and returns immediately. No Deployment, no PA, nothing is created until all image tags are pinned to digests. This is by design — Deployments with mutable tags (`myapp:latest`) would allow silent rollouts if the tag moves.

#### Phase 2 — Optional System-Internal TLS Certificate

```go
    if config.FromContext(ctx).Network.SystemInternalTLSEnabled() {
        if err := c.reconcileQueueProxyCertificate(ctx, rev); err != nil {
            return err
        }
    }
```

When system-internal TLS is enabled (mTLS between components), a per-namespace TLS certificate must exist for the queue-proxy before pods can start. If the certificate isn't ready yet, reconciliation stops here and will be retried when the Certificate resource changes (via the tracker wired up in the controller).

#### Phase 3 — Resource Creation Pipeline

```go
    for _, phase := range []func(context.Context, *v1.Revision) error{
        c.reconcileDeployment,
        c.reconcileImageCache,
        c.reconcilePA,
    } {
        if err := phase(ctx, rev); err != nil {
            return err
        }
    }
```

These three phases run sequentially. If any fails it returns an error (the framework re-queues). They are ordered: Deployment first (so `reconcilePA` can read the Deployment's status when building the PA spec), then image cache (fire-and-forget, doesn't block readiness), then PA.

#### Phase 4 — Ready Transition Events

```go
    readyAfterReconcile := rev.Status.GetCondition(v1.RevisionConditionReady).IsTrue()
    if !readyBeforeReconcile && readyAfterReconcile {
        controller.GetEventRecorder(ctx).Event(rev, corev1.EventTypeNormal, "RevisionReady",
            "Revision becomes ready upon all resources being ready")
    } else if readyBeforeReconcile && !readyAfterReconcile {
        logger.Info("Revision stopped being ready")
    }
    return nil
}
```

### 7.4 reconcileDigest() — The Background Resolver

```go
// pkg/reconciler/revision/revision.go:80-119
func (c *Reconciler) reconcileDigest(ctx context.Context, rev *v1.Revision) (bool, error) {
    totalNumOfContainers := len(rev.Spec.Containers) + len(rev.Spec.InitContainers)

    // Fast path: all digests already resolved and stored in status
    if len(rev.Status.ContainerStatuses)+len(rev.Status.InitContainerStatuses) == totalNumOfContainers {
        c.resolver.Clear(types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name})
        return true, nil  // done, clear in-flight state
    }

    // Build auth options from the revision's service account and pull secrets
    opt := k8schain.Options{
        Namespace:          rev.Namespace,
        ServiceAccountName: rev.Spec.ServiceAccountName,
        ImagePullSecrets:   imagePullSecrets,
    }

    initStatuses, statuses, err := c.resolver.Resolve(logger, rev, opt,
        cfgs.Deployment.RegistriesSkippingTagResolving,
        cfgs.Deployment.DigestResolutionTimeout)
    if err != nil {
        c.resolver.Clear(...)  // clear so next reconcile retries
        rev.Status.MarkContainerHealthyFalse(v1.ReasonContainerMissing, err.Error())
        return true, err  // return true+err: error IS the answer, no need to wait
    }

    if len(statuses) > 0 || len(initStatuses) > 0 {
        // Digests came back — store them and let reconciliation continue
        rev.Status.ContainerStatuses     = statuses
        rev.Status.InitContainerStatuses = initStatuses
        return true, nil
    }

    // nil, nil from resolver means "in-flight, not ready yet"
    return false, nil
}
```

The `backgroundResolver.Resolve()` has three possible return shapes, each meaning something different:

| Return | Meaning | Action |
|--------|---------|--------|
| `nil, nil, nil` | Work item submitted or already in-flight | Return `false` — exit, wait for re-enqueue |
| `statuses, nil` | Digests ready | Return `true, nil` — proceed to create Deployment |
| `nil, err` | Resolution failed (image not found, auth error) | Return `true, err` — surface the error |

The background resolver lifecycle for a single Revision:

```
ReconcileKind call #1:
  ContainerStatuses is empty
  resolver.Resolve() → result not in map → addWorkItems()
    submits workItem{image: "myapp:latest"} to digestResolveQueue
  returns nil, nil, nil → reconcileDigest returns false, nil
  ReconcileKind returns nil (sets ResourcesAvailable=Unknown)

[Background goroutine runs]:
  Calls registry API: "myapp:latest" → "myapp@sha256:abc123"
  Stores result in r.results[namespace/rev-name]
  Calls completionCallback → impl.EnqueueKey(namespace/rev-name)

ReconcileKind call #2:
  ContainerStatuses is still empty
  resolver.Resolve() → result IS in map AND is ready
  returns ContainerStatuses=[{Name:"user-container", ImageDigest:"myapp@sha256:abc123"}]
  reconcileDigest returns true, nil
  Reconciliation proceeds to create Deployment

ReconcileKind call #3+ (Deployment exists, ContainerStatuses already in status):
  len(ContainerStatuses) == totalNumOfContainers
  resolver.Clear() — removes the entry from the results map
  returns true immediately — skips the resolver entirely
```

`RegistriesSkippingTagResolving` is a configurable set of registry hostnames. Tags from these registries are not resolved to digests (useful for local development or air-gapped environments).

### 7.5 MakeDeployment() — What Gets Created

```go
// pkg/reconciler/revision/resources/deploy.go:352-406
func MakeDeployment(rev *v1.Revision, cfg *config.Config) (*appsv1.Deployment, error) {
    podSpec, err := makePodSpec(rev, cfg)  // user containers + queue-proxy sidecar

    replicaCount := cfg.Autoscaler.InitialScale  // default initial replicas
    if ann, found := autoscaling.InitialScaleAnnotation.Get(rev.Annotations); found {
        replicaCount = int32(ann)  // user can override via annotation
    }

    progressDeadline := int32(cfg.Deployment.ProgressDeadline.Seconds())
    if pdAnn, found := serving.ProgressDeadlineAnnotation.Get(rev.Annotations); found {
        progressDeadline = int32(pdAnn)  // user can override via annotation
    }

    maxUnavailable := intstr.FromInt(0)  // rolling update: never take pods offline

    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:            names.Deployment(rev),  // "myapp-00002-deployment"
            Namespace:       rev.Namespace,
            OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
        },
        Spec: appsv1.DeploymentSpec{
            Replicas:                ptr.Int32(replicaCount),
            ProgressDeadlineSeconds: ptr.Int32(progressDeadline),
            RevisionHistoryLimit:    ptr.Int32(0),  // don't keep old ReplicaSets
            Strategy: appsv1.DeploymentStrategy{
                Type: appsv1.RollingUpdateDeploymentStrategyType,
                RollingUpdate: &appsv1.RollingUpdateDeployment{
                    MaxUnavailable: &maxUnavailable,  // 0: add new before removing old
                },
            },
            Template: corev1.PodTemplateSpec{Spec: *podSpec},
        },
    }, nil
}
```

Key design decisions in the Deployment spec:
- `RevisionHistoryLimit: 0` — Kubernetes keeps no old ReplicaSets; Knative manages history via Revisions
- `MaxUnavailable: 0` — during updates, new pods are added before old ones are removed, preventing traffic disruption
- `InitialScale` — the Deployment starts with a non-zero replica count so the autoscaler can determine the revision is healthy before scaling it down

#### The Queue-Proxy Sidecar

`makePodSpec` injects a `queue-proxy` sidecar into every pod alongside the user's containers:

```
Pod containers after injection:
┌─────────────────────────────────────────────────────────────────┐
│  user-container (from RevisionSpec.Containers[0])               │
│  - the actual application                                       │
│  - PreStop hook calls queue-proxy drain endpoint                │
│    to block shutdown until in-flight requests finish            │
├─────────────────────────────────────────────────────────────────┤
│  queue-proxy (injected by MakeDeployment)                       │
│  - sits in front of user-container                              │
│  - enforces ContainerConcurrency limits                         │
│  - reports request metrics to the autoscaler                    │
│  - exposes /health, /metrics, /drain endpoints                  │
│  - handles system-internal TLS termination                      │
└─────────────────────────────────────────────────────────────────┘

Shared volumes injected:
  - knative-var-log    → /var/log (log collection if enabled)
  - knative-token-volume → /var/run/secrets/tokens (service account tokens)
  - server-certs       → /var/lib/knative/certs (system-internal TLS)
  - pod-info           → /etc/podinfo (downward API: pod annotations)
```

The `userLifecycle` PreStop hook is also injected into user containers:
```go
// pkg/reconciler/revision/resources/deploy.go:101-113
userLifecycle = &corev1.Lifecycle{
    PreStop: &corev1.LifecycleHandler{
        HTTPGet: &corev1.HTTPGetAction{
            Port: intstr.FromInt(networking.QueueAdminPort),
            Path: queue.RequestQueueDrainPath,  // "/drain"
        },
    },
}
```
When Kubernetes signals a pod to stop, it calls this hook before sending `SIGTERM` to the user process. The hook blocks until the queue-proxy has drained all in-flight requests, ensuring graceful shutdown.

### 7.6 reconcileDeployment() — Create, Update, and Crash Detection

```go
// pkg/reconciler/revision/reconcile_resources.go:58-142
func (c *Reconciler) reconcileDeployment(ctx context.Context, rev *v1.Revision) error {
    deploymentName := resourcenames.Deployment(rev)  // "myapp-00002-deployment"

    deployment, err := c.deploymentLister.Deployments(ns).Get(deploymentName)
    if apierrs.IsNotFound(err) {
        rev.Status.MarkResourcesAvailableUnknown(v1.ReasonDeploying, "")
        rev.Status.MarkContainerHealthyUnknown(v1.ReasonDeploying, "")
        c.createDeployment(ctx, rev)
        return nil
    }

    if !metav1.IsControlledBy(deployment, rev) {
        rev.Status.MarkResourcesAvailableFalse(v1.ReasonNotOwned,
            v1.ResourceNotOwnedMessage("Deployment", deploymentName))
        return error
    }

    // Semantic diff and update
    deployment, err = c.checkAndUpdateDeployment(ctx, rev, deployment)

    // Propagate Deployment conditions → Revision conditions
    rev.Status.PropagateDeploymentStatus(&deployment.Status)
```

After propagating deployment status, the reconciler checks for **crashing containers** — a scenario where the Deployment has replicas requested but none are available:

```go
    if *deployment.Spec.Replicas > 0 && deployment.Status.AvailableReplicas == 0 {
        // Get one pod to diagnose
        pods, _ := c.kubeclient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
            LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
            Limit: 1,
        })

        if len(pods.Items) > 0 {
            pod := pods.Items[0]

            // Check for scheduling failures (insufficient resources)
            for _, cond := range pod.Status.Conditions {
                if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
                    rev.Status.MarkResourcesAvailableFalse(cond.Reason, cond.Message)
                }
            }

            // Check for container exit codes (CrashLoopBackOff, OOMKilled, etc.)
            for _, status := range pod.Status.ContainerStatuses {
                if status.Name != resources.QueueContainerName {  // skip queue-proxy
                    if t := status.LastTerminationState.Terminated; t != nil {
                        rev.Status.MarkContainerHealthyFalse(
                            v1.ExitCodeReason(t.ExitCode),           // "ExitCode137"
                            v1.RevisionContainerExitingMessage(t.Message))
                    } else if w := status.State.Waiting; w != nil && hasDeploymentTimedOut(deployment) {
                        rev.Status.MarkResourcesAvailableFalse(w.Reason, w.Message)
                    }
                }
            }
        }
    }

    if deployment.Status.ReadyReplicas > 0 {
        rev.Status.MarkContainerHealthyTrue()
    }
}
```

The crash detection only inspects the **user container** (`status.Name != QueueContainerName`). Queue-proxy failures are surfaced differently. This gives users meaningful error messages like `ExitCode137` (OOM kill) or `ImagePullBackOff` in the Revision's status rather than having to dig into pod events.

### 7.7 MakePA() and reconcilePA() — Scaling Policy

```go
// pkg/reconciler/revision/resources/pa.go:32-52
func MakePA(rev *v1.Revision, deployment *appsv1.Deployment) *autoscalingv1alpha1.PodAutoscaler {
    return &autoscalingv1alpha1.PodAutoscaler{
        ObjectMeta: metav1.ObjectMeta{
            Name:            names.PA(rev),   // same name as Revision
            Namespace:       rev.Namespace,
            OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
        },
        Spec: autoscalingv1alpha1.PodAutoscalerSpec{
            ContainerConcurrency: rev.Spec.GetContainerConcurrency(),
            ScaleTargetRef: corev1.ObjectReference{
                APIVersion: "apps/v1",
                Kind:       "Deployment",
                Name:       names.Deployment(rev),  // "myapp-00002-deployment"
            },
            ProtocolType: rev.GetProtocol(),          // HTTP1, HTTP2, or GRPC
            Reachability: reachability(rev, deployment),
        },
    }
}
```

The `Reachability` field is derived from the Revision's `RoutingState` label:

```go
// pkg/reconciler/revision/resources/pa.go:54-82
func reachability(rev *v1.Revision, deployment *appsv1.Deployment) autoscalingv1alpha1.ReachabilityType {
    // If infra is broken and no pods are running, mark unreachable immediately
    if infraFailure && *deployment.Spec.Replicas > 0 && deployment.Status.ReadyReplicas == 0 {
        return autoscalingv1alpha1.ReachabilityUnreachable
    }

    switch rev.GetRoutingState() {
    case v1.RoutingStateActive:   return autoscalingv1alpha1.ReachabilityReachable
    case v1.RoutingStateReserve:  return autoscalingv1alpha1.ReachabilityUnreachable
    default:                      return autoscalingv1alpha1.ReachabilityUnknown
    }
}
```

`Reachability` tells the autoscaler how urgently it should scale:
- `Reachable` — traffic is being sent to this Revision; scale aggressively
- `Unreachable` — no traffic is going here; scale to zero immediately
- `Unknown` — newly created, pending route assignment; hold at initial scale

The `reconcilePA` function also syncs autoscaling annotations from the Revision onto the PA:

```go
// pkg/reconciler/revision/reconcile_resources.go:200-219
if !equality.Semantic.DeepEqual(tmpl.Spec, pa.Spec) || annotationsNeedReconcilingForKPA(pa.Annotations, tmpl.Annotations) {
    want := pa.DeepCopy()
    want.Spec = tmpl.Spec
    syncAnnotationsForKPA(want.Annotations, tmpl.Annotations)
    c.client.AutoscalingV1alpha1().PodAutoscalers(ns).Update(ctx, want, metav1.UpdateOptions{})
}
```

`syncAnnotationsForKPA` handles the autoscaling annotation semantics: user-defined autoscaling annotations on the Revision (e.g. `autoscaling.knative.dev/minScale: "2"`) are forwarded onto the PA, while defaulted KPA annotations (class, metric) are preserved. This allows live updates to scaling configuration without creating a new Revision.

### 7.8 reconcileImageCache() — Pre-pull Optimization

```go
// pkg/reconciler/revision/reconcile_resources.go:144-162
func (c *Reconciler) reconcileImageCache(ctx context.Context, rev *v1.Revision) error {
    ns := rev.Namespace
    // For each resolved container image digest
    for _, container := range append(rev.Status.ContainerStatuses, rev.Status.InitContainerStatuses...) {
        imageName := kmeta.ChildName(resourcenames.ImageCache(rev), "-"+container.Name)
        if _, err := c.imageLister.Images(ns).Get(imageName); apierrs.IsNotFound(err) {
            c.createImageCache(ctx, rev, container.Name, container.ImageDigest)
        }
    }
    return nil
}
```

An `Image` object (from `caching.knative.dev/v1alpha1`) is a request to pre-pull the resolved image digest onto cluster nodes. The caching controller watches these objects and maintains a DaemonSet that pre-pulls the image.

This means that if a Revision is scaled to zero and then receives traffic, the image may already be on the node — reducing cold-start latency significantly. Note: `reconcileImageCache` only creates Image objects; it does not update them (Revisions are immutable, so the digest never changes) and does not block readiness on them.

### 7.9 ObserveDeletion() — Cleanup on Revision Deletion

```go
// pkg/reconciler/revision/revision.go:190-193
func (c *Reconciler) ObserveDeletion(ctx context.Context, key types.NamespacedName) error {
    c.resolver.Forget(key)
    return nil
}
```

When a Revision is deleted, `ObserveDeletion` is called instead of `ReconcileKind`. It calls `resolver.Forget` which:
1. Walks the pending work items for this Revision
2. Calls `queue.Forget(item)` on each — removes them from the rate limiter's backoff tracking
3. Deletes the entry from the `results` map

Without this, a long-running digest resolution for a deleted Revision would eventually call `impl.EnqueueKey` on a key that no longer exists — wasting a reconcile cycle and leaking memory in the results map.

### 7.10 Revision Reconciler State Machine

```
Revision created by Configuration reconciler
          │
          ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Phase 1: reconcileDigest()                              │
    │                                                          │
    │  ContainerStatuses already full?                         │
    │    YES → resolver.Clear(), proceed                       │
    │                                                          │
    │    NO  → resolver.Resolve()                              │
    │          Already in results map?                         │
    │            Result ready?                                 │
    │              YES, no error → store statuses, proceed     │
    │              YES, error    → MarkContainerHealthyFalse   │
    │            Not ready yet → return false                  │
    │          Not in map → addWorkItems() to digest queue     │
    │                       return false (exit, wait)          │
    │                                                          │
    │  ResourcesAvailable=Unknown("ResolvingDigests")          │
    └──────────────────────────────────────────────────────────┘
          │ (only if digests resolved)
          ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Phase 2 (conditional): reconcileQueueProxyCertificate() │
    │  Only runs when SystemInternalTLS is enabled             │
    │  Certificate not ready → return error (retry)           │
    └──────────────────────────────────────────────────────────┘
          │
          ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Phase 3a: reconcileDeployment()                         │
    │                                                          │
    │  Deployment not found?                                   │
    │    → MakeDeployment() (user containers + queue-proxy)    │
    │    → MarkResourcesAvailableUnknown("Deploying")          │
    │  Deployment found, not owned? → MarkFalse("NotOwned")   │
    │  Deployment found, owned?                                │
    │    → checkAndUpdateDeployment() (semantic diff)          │
    │    → PropagateDeploymentStatus()                         │
    │  Replicas>0 but AvailableReplicas==0?                    │
    │    → inspect pod: scheduling failure? CrashLoop?         │
    │    → MarkResourcesAvailableFalse or MarkContainerFalse   │
    │  ReadyReplicas>0 → MarkContainerHealthyTrue()           │
    └──────────────────────────────────────────────────────────┘
          │
          ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Phase 3b: reconcileImageCache()                         │
    │  For each resolved digest: create Image object if absent │
    │  Never blocks readiness                                  │
    └──────────────────────────────────────────────────────────┘
          │
          ▼
    ┌──────────────────────────────────────────────────────────┐
    │  Phase 3c: reconcilePA()                                 │
    │                                                          │
    │  PA not found?                                           │
    │    → MakePA() (ContainerConcurrency, ScaleTargetRef,     │
    │                Reachability from RoutingState)           │
    │  PA found, not owned? → MarkFalse("NotOwned")           │
    │  PA found, owned?                                        │
    │    → PropagateAutoscalerStatus() → copies ActualReplicas │
    │    → diff PA spec + KPA annotations, update if changed   │
    └──────────────────────────────────────────────────────────┘
          │
          ▼
    Emit "RevisionReady" event if Ready transitioned True→False
    return nil
    Framework calls UpdateStatus()
```

---

_Next: Section 8 — Route Reconciler_

---

## 8. Route Reconciler

**Files:**
- [pkg/reconciler/route/route.go](pkg/reconciler/route/route.go)
- [pkg/reconciler/route/controller.go](pkg/reconciler/route/controller.go)
- [pkg/reconciler/route/traffic/traffic.go](pkg/reconciler/route/traffic/traffic.go)
- [pkg/reconciler/route/resources/](pkg/reconciler/route/resources/)

### 8.1 Responsibility

The Route reconciler is the networking layer of Knative Serving. Its job is to:
1. **Resolve traffic targets** — translate `ConfigurationName` references to concrete `RevisionName`s
2. **Create placeholder Kubernetes Services** — one per traffic target, giving each a stable DNS name
3. **Provision TLS certificates** — for external domains and/or cluster-local domains
4. **Create/update the Knative Ingress** — the `networking.knative.dev/Ingress` object that the ingress controller (Istio/Contour/Kourier) programs into the actual load balancer
5. **Propagate ingress status** back to the Route's own conditions

It never creates Revisions, Deployments, or PodAutoscalers. It only programs the network path to existing, ready pods.

### 8.2 The Reconciler Struct

```go
// pkg/reconciler/route/route.go:65-81
type Reconciler struct {
    kubeclient kubernetes.Interface    // creates placeholder k8s Services
    client     clientset.Interface     // reads Configurations/Revisions
    netclient  netclientset.Interface  // creates Knative Ingress and Certificates

    configurationLister listers.ConfigurationLister
    revisionLister      listers.RevisionLister
    serviceLister       corev1listers.ServiceLister      // placeholder k8s Services
    endpointsLister     corev1listers.EndpointsLister
    ingressLister       networkinglisters.IngressLister
    certificateLister   networkinglisters.CertificateLister
    tracker             tracker.Interface  // watches Configurations/Revisions it references

    clock        clock.PassiveClock
    enqueueAfter func(interface{}, time.Duration)  // for rollout pacing
}
```

The Route reconciler has the most listers of any core reconciler because it bridges two worlds: the Serving API (Configurations, Revisions) and the networking API (Ingress, Certificate).

`enqueueAfter` is used during gradual rollouts — the reconciler schedules itself to re-run after a fixed interval to advance the traffic split incrementally.

### 8.3 ReconcileKind — Annotated Full Walk-through

#### New Generation Guard

```go
// pkg/reconciler/route/route.go:116-118
if r.GetObjectMeta().GetGeneration() != r.Status.ObservedGeneration {
    r.Status.MarkIngressNotConfigured()
}
```

On the first reconcile of a new generation, the Route immediately marks `IngressReady=Unknown`. Without this, if reconciliation fails partway through, the old `IngressReady=True` from the previous generation would persist — giving a false signal to the Service reconciler.

#### Step 1 — Configure Traffic

```go
traffic, err := c.configureTraffic(ctx, r)
if traffic == nil || err != nil {
    // Traffic targets not ready — exit early
    return err
}
```

`configureTraffic` is the most consequential step. If it returns `nil, nil` (bad traffic targets), reconciliation stops entirely — there is no point creating an Ingress if we don't know where traffic should go. Covered in detail in 8.4.

#### Step 2 — Set the Cluster-Internal Address

```go
// pkg/reconciler/route/route.go:134-139
r.Status.Address = &duckv1.Addressable{
    URL: &apis.URL{
        Scheme: "http",
        Host:   resourcenames.K8sServiceFullname(r),
    },
}
```

Every Route gets a stable cluster-internal address in the form `<route-name>.<namespace>.svc.cluster.local`. This is the address used by other cluster services to talk to the Route without going through the external ingress. It's set unconditionally before TLS is resolved because this address is always HTTP (internal mTLS is handled separately via system-internal TLS).

#### Step 3 — Reconcile Placeholder Services

```go
services, err := c.reconcilePlaceholderServices(ctx, r, traffic.Targets)
```

Placeholder services are explained in detail in 8.5.

#### Step 4 — TLS Certificate Provisioning

```go
tls, acmeChallenges, desiredCerts, err := c.externalDomainTLS(ctx, r.Status.URL.Host, r, traffic)

if config.FromContext(ctx).Network.ClusterLocalDomainTLS == netcfg.EncryptionEnabled {
    internalTLS, _ := c.clusterLocalDomainTLS(ctx, r, traffic)
    tls = append(tls, internalTLS...)
}
```

`tls` is a slice of `IngressTLS` structs that will be embedded directly in the Knative Ingress spec. Covered in 8.6.

#### Step 5 — Reconcile the Knative Ingress

```go
ingress, effectiveRO, err := c.reconcileIngress(ctx, r, traffic, tls,
    ingressClassForRoute(ctx, r), acmeChallenges...)
```

`ingressClassForRoute` reads the `networking.knative.dev/ingress-class` annotation on the Route, falling back to the cluster-wide `defaultIngressClass` from config. This allows different Routes to use different ingress implementations.

`effectiveRO` is the effective Rollout object — tracking gradual traffic shifts during canary deployments.

#### Step 6 — Propagate Ingress Status

```go
roInProgress := !effectiveRO.Done()
if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration {
    r.Status.MarkIngressNotConfigured()
} else if !roInProgress {
    r.Status.PropagateIngressStatus(ingress.Status)
}
```

The same generation guard pattern from Section 3.4 applies here: if the Ingress controller hasn't processed the latest Ingress spec yet, its status conditions reflect the old spec. The Route marks itself `IngressNotConfigured` rather than trusting stale data.

If a rollout is in progress, `PropagateIngressStatus` is skipped — the Route stays in `Unknown` state until the rollout completes.

#### Step 7 — Update Placeholder Services

```go
c.updatePlaceholderServices(ctx, r, services, ingress)
```

After the Ingress is created, placeholder services are updated with its load-balancer information (covered in 8.5).

#### Step 8 — Rollout Handling

```go
if roInProgress {
    r.Status.MarkIngressRolloutInProgress()
    r.Status.Traffic, err = traffic.GetRevisionTrafficTargets(ctx, r, effectiveRO)
    c.enqueueAfter(r, rolloutInterval)  // re-run after a delay to advance the rollout
    return nil
}
```

During a canary rollout (e.g. shifting from 0% to 100% over 10 minutes), the reconciler re-enqueues itself on a timer. Each run advances the traffic split by a calculated increment and updates the Ingress.

### 8.4 configureTraffic() — Resolving Traffic Targets

```go
// pkg/reconciler/route/route.go:423-494
func (c *Reconciler) configureTraffic(ctx context.Context, r *v1.Route) (*traffic.Config, error) {

    // Step 1: Build the full traffic configuration from the Route spec.
    // This resolves ConfigurationName → LatestReadyRevisionName
    // and validates all targets exist and are ready.
    t, trafficErr := traffic.BuildTrafficConfiguration(
        c.configurationLister, c.revisionLister, r)

    if t == nil {
        return nil, trafficErr  // completely failed to build (informer not synced, etc.)
    }

    // Step 2: Determine visibility (public vs cluster-local) for each target
    visibilityMap, _ := visibility.NewResolver(c.serviceLister).GetVisibility(ctx, r)
    t.Visibility = visibilityMap

    // Step 3: Set Route.Status.URL based on visibility
    c.updateRouteStatusURL(ctx, r, t.Visibility)

    // Step 4: Register tracker watches for all referenced objects
    // This ensures the Route is re-reconciled when:
    // - A missing target appears in the informer cache
    // - A Configuration's LatestReadyRevisionName changes
    // - A referenced Revision's status changes
    for _, obj := range t.MissingTargets {
        c.tracker.TrackReference(tracker.Reference{...obj...}, r)
    }
    for _, configuration := range t.Configurations {
        c.tracker.TrackReference(objectRef(configuration), r)
    }
    for _, revision := range t.Revisions {
        c.tracker.TrackReference(objectRef(revision), r)
    }

    // Step 5: Handle traffic target errors
    var badTarget traffic.TargetError
    if errors.As(trafficErr, &badTarget) {
        badTarget.MarkBadTrafficTarget(&r.Status)  // sets AllTrafficAssigned=False or Unknown
        return nil, nil  // return nil Config to stop further reconciliation
    }

    // Step 6: All targets valid and ready — build the resolved status traffic table
    r.Status.Traffic, _ = t.GetRevisionTrafficTargets(ctx, r, &traffic.Rollout{})
    r.Status.MarkTrafficAssigned()

    return t, nil
}
```

`BuildTrafficConfiguration` walks the `Route.Spec.Traffic` slice and for each entry:
- If `ConfigurationName` is set: looks up the Configuration, reads `LatestReadyRevisionName`
- If `RevisionName` is set: looks up the Revision directly
- Returns a `Config` with:
  - `Targets`: grouped by tag name (unnamed targets under `""`)
  - `Configurations`: all referenced Configurations (for tracker registration)
  - `Revisions`: all resolved Revisions
  - `MissingTargets`: references that couldn't be found yet (informer not warmed, or truly missing)

The **tracker registration** in Step 4 is what makes the Route self-healing. When a Configuration's `LatestReadyRevisionName` changes (because a new Revision just became ready), `tracker.OnChanged` fires → Route is re-enqueued → `configureTraffic` runs again → the new revision is now in the traffic table → Ingress is updated → traffic shifts.

### 8.5 Placeholder Kubernetes Services — Why They Exist

For every traffic target (named and unnamed), the Route creates a Kubernetes `Service` object:

```
Route "myapp" with traffic:
  - "" (default): 90% → myapp-00001
  - "canary": 10% → myapp-00002

Creates Services:
  - "myapp"         (namespace: default) — for the default "" target
  - "myapp-canary"  (namespace: default) — for the "canary" tagged target
```

These are **placeholder** services — they have no selectors and no endpoints of their own. Their purpose is:

1. **Stable DNS names**: `myapp.default.svc.cluster.local` resolves regardless of which revision is currently active. Clients and service meshes that use DNS-based discovery always have a valid address.

2. **Tagged URLs**: `myapp-canary.default.svc.cluster.local` gives the canary target its own addressable endpoint. The Route publishes this as `traffic[i].url` in its status.

3. **Ingress backing**: The actual traffic routing is done by the Knative Ingress (which programs Istio/Contour/Kourier). The placeholder Services are the Kubernetes objects that the ingress controller references when creating virtual services or proxy rules.

After the Ingress is created, `updatePlaceholderServices` sets annotations on the placeholder services with the Ingress's load-balancer addresses. This makes `kubectl get ksvc` show the correct external IP/hostname in `EXTERNAL-IP`.

### 8.6 TLS Certificate Provisioning

The Route reconciler handles three distinct TLS scenarios:

#### External Domain TLS

```go
// pkg/reconciler/route/route.go:203-317
func (c *Reconciler) externalDomainTLS(...) {
    if !externalDomainTLSEnabled(ctx, r) {
        r.Status.MarkTLSNotEnabled(v1.ExternalDomainTLSNotEnabledMessage)
        return tls, nil, desiredCerts, nil  // TLS disabled — mark True with reason
    }

    // Get all domains for this route (main domain + tagged sub-domains)
    domainToTagMap, _ := domains.GetAllDomainsAndTags(ctx, r, trafficNames, visibility)

    // Try to match an existing wildcard cert before creating a new one
    // (avoids Let's Encrypt rate limits)
    allWildcardCerts, _ := c.certificateLister.List(wildcardCertSelector)

    desiredCerts = resources.MakeCertificates(r, domainToTagMap, certClass, routeDomain)
    for _, desiredCert := range desiredCerts {
        cert := findMatchingWildcardCert(ctx, desiredCert.Spec.DNSNames, allWildcardCerts)

        if cert == nil {
            cert, _ = networkaccessor.ReconcileCertificate(ctx, r, desiredCert, c)
        }

        if cert.IsReady() {
            r.Status.URL.Scheme = "https"           // upgrade to HTTPS
            r.Status.MarkCertificateReady(cert.Name)
            tls = append(tls, resources.MakeIngressTLS(cert, dnsNames))
        } else if cert.IsFailed() {
            r.Status.MarkCertificateProvisionFailed(cert)
        } else {
            // Cert pending — check if HTTP downgrade is allowed
            acmeChallenges = append(acmeChallenges, cert.Status.HTTP01Challenges...)
            if httpProtocol == netcfg.HTTPEnabled {
                r.Status.URL.Scheme = "http"  // temporarily downgrade
                r.Status.MarkHTTPDowngrade(cert.Name)
            }
        }
    }

    c.deleteOrphanRouteCerts(ctx, r, domainToTagMap, ...)
}
```

Key design decisions:
- **Wildcard cert reuse**: Before creating a new cert, the reconciler checks if an existing wildcard cert covers the desired domains. This is critical for multi-tenant clusters where each Route under `*.example.com` would otherwise create a redundant certificate, quickly exhausting Let's Encrypt rate limits.
- **HTTP downgrade**: While a certificate is being provisioned (ACME challenge in progress), if `httpProtocol=enabled`, the Route stays accessible on HTTP. This avoids a window of unavailability during TLS adoption.
- **ACME HTTP01 challenges**: During certificate issuance, the ACME provider sets HTTP01 challenge records on the Ingress so the Let's Encrypt servers can validate domain ownership.

#### Cluster-Local Domain TLS

```go
// pkg/reconciler/route/route.go:319-365
func (c *Reconciler) clusterLocalDomainTLS(...) {
    for name := range tc.Targets {
        localDomains, _ := domains.GetDomainsForVisibility(ctx, name, r, ClusterLocal)
        desiredCert := resources.MakeClusterLocalCertificate(r, name, localDomains, certClass)
        cert, _ := networkaccessor.ReconcileCertificate(ctx, r, desiredCert, c)

        if cert.IsReady() {
            r.Status.Address.URL.Scheme = "https"  // upgrade cluster-internal address
            tls = append(tls, resources.MakeIngressTLS(cert, localDomains))
        }
    }
}
```

This is separate from external TLS — it secures the `*.svc.cluster.local` addresses used by in-cluster clients.

#### Orphan Certificate Cleanup

```go
// pkg/reconciler/route/route.go:368-415
func (c *Reconciler) deleteOrphanRouteCerts(...) {
    // List all certs owned by this Route
    certs, _ := c.certificateLister.List(routeLabelSelector)

    for _, cert := range certs {
        // If this cert's DNS names are no longer in the current domain map → delete it
        if !isStillNeeded(cert, domainToTagMap) {
            certClient.Certificates(cert.Namespace).Delete(ctx, cert.Name, ...)
        }
    }
}
```

When a Route's traffic configuration changes (e.g. a named tag is removed), the certificate for that tag's subdomain is no longer needed. Without this cleanup, orphaned certificates would accumulate indefinitely.

### 8.7 The Knative Ingress Object

The `Ingress` (from `networking.knative.dev/v1alpha1`) is Knative's abstraction over the cluster's ingress implementation. It is not a Kubernetes `networking.k8s.io/Ingress` — it has richer semantics for traffic splitting and TLS.

```
Knative Ingress created by the Route reconciler:
┌─────────────────────────────────────────────────────────────────┐
│ spec:                                                           │
│   rules:                                                        │
│   - hosts: ["myapp.default.example.com"]  # external domain    │
│     visibility: ExternalIP                                      │
│     http:                                                       │
│       paths:                                                    │
│       - splits:                                                 │
│         - serviceName: myapp-00001     # pod service           │
│           servicePort: 80                                       │
│           percent: 90                                           │
│         - serviceName: myapp-00002                              │
│           servicePort: 80                                       │
│           percent: 10                                           │
│   - hosts: ["myapp.default.svc.cluster.local"]  # internal     │
│     visibility: ClusterLocal                                    │
│     http:                                                       │
│       paths:                                                    │
│       - splits:                                                 │
│         - serviceName: myapp-00001                              │
│           percent: 90                                           │
│         - serviceName: myapp-00002                              │
│           percent: 10                                           │
│   tls:                                                          │
│   - hosts: ["myapp.default.example.com"]                        │
│     secretName: route-myapp-abc123                              │
└─────────────────────────────────────────────────────────────────┘
         │
         │ ingress-controller (Istio / Contour / Kourier) reads this
         ▼
Programs actual load balancer rules, virtual services, etc.
Updates Ingress.Status.LoadBalancer with external IP/hostname
         │
         ▼
Route reconciler reads Ingress.Status
→ PropagateIngressStatus() → Route.IngressReady = True
→ Route.Status.URL.Scheme = "https" (if TLS ready)
```

The `serviceName` in the Ingress splits refers to the **per-revision** headless services that the Knative networking layer creates for each PodAutoscaler's ServerlessService — not the placeholder services. The placeholder services provide DNS, while the per-revision services provide actual pod-level routing.

### 8.8 Route Reconciler State Machine

```
Route created/updated (new Generation or child resource changed)
          │
          ▼
    Mark IngressNotConfigured (new generation guard)
          │
          ▼
    configureTraffic(ctx, r)
    ┌─────────────────────────────────────────────────────────────┐
    │ BuildTrafficConfiguration()                                  │
    │   Resolve each TrafficTarget:                               │
    │     ConfigurationName → LatestReadyRevisionName             │
    │     RevisionName      → direct lookup                       │
    │                                                             │
    │   MissingTargets? → track them (retry when they appear)     │
    │   BadTarget (Not Ready / Failed)?                           │
    │     → MarkAllTrafficAssigned False/Unknown, return nil,nil  │
    │                                                             │
    │   All ready → MarkTrafficAssigned=True                      │
    │             → r.Status.Traffic = resolved RevisionTargets   │
    │             → Register tracker for all Configs+Revisions    │
    └─────┬───────────────────────────────────────────────────────┘
          │ (only continues if traffic != nil)
          ▼
    Set r.Status.Address (cluster-internal http URL)
          │
          ▼
    reconcilePlaceholderServices()
    Create/update one k8s Service per named+unnamed traffic target
          │
          ▼
    externalDomainTLS()
    ┌────────────────────────────────────────────────────────────┐
    │ TLS disabled? → MarkTLSNotEnabled=True, tls=[]             │
    │ TLS enabled?                                               │
    │   For each domain:                                         │
    │     Wildcard cert matches? → reuse it                      │
    │     No match → ReconcileCertificate()                      │
    │       Cert Ready? → append to tls[], set scheme=https      │
    │       Cert Failed? → MarkCertificateProvisionFailed        │
    │       Cert Pending? → collect ACME challenges              │
    │                       HTTP downgrade if httpProtocol=on    │
    │   deleteOrphanRouteCerts()                                  │
    └─────┬──────────────────────────────────────────────────────┘
          │
          ▼
    clusterLocalDomainTLS() (if enabled)
          │
          ▼
    reconcileIngress(r, traffic, tls, ingressClass, acmeChallenges)
    Create or update Knative Ingress with all splits and TLS
          │
          ▼
    Ingress generation guard:
      Ingress.Gen != Ingress.Status.ObservedGen?
        → MarkIngressNotConfigured
      Rollout in progress?
        → MarkIngressRolloutInProgress
        → Update Status.Traffic with rollout percentages
        → enqueueAfter(rolloutInterval)
        → return nil
      Otherwise:
        → PropagateIngressStatus()
          (IngressReady = True/False/Unknown from Ingress.Status)
          │
          ▼
    updatePlaceholderServices()
    Annotate placeholder Services with load-balancer addresses
          │
          ▼
    return nil — framework calls UpdateStatus()
```

### 8.9 What the Route Reconciler Does NOT Do

| Concern | Owned by |
|---------|----------|
| Creating Revisions | Configuration reconciler |
| Scaling pods | KPA / autoscaler |
| Activating scaled-to-zero revisions | Activator + SKS reconciler |
| Actually programming the load balancer | Ingress controller (Istio/Contour/Kourier) |
| Issuing TLS certificates | Certificate controller (cert-manager) |
| Cleaning up old Revisions | GC reconciler |
| Updating Revision RoutingState labels | Labeler reconciler |

The Route reconciler creates the `Ingress` and `Certificate` objects as *desired state* declarations. The actual work (programming Envoy/NGINX/iptables, calling the ACME API) is done by separate controllers that watch those objects.

---

---

## 9. End-to-End Walkthrough

This section follows a single `kubectl apply` command — from the moment the API server writes the Service object to the moment traffic is being served — showing exactly which reconciler runs, what it reads, what it creates, and what event triggers the next step.

### 9.1 The Scenario

```yaml
# my-service.yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: hello
  namespace: default
spec:
  template:
    spec:
      containers:
        - image: gcr.io/knative-samples/helloworld-go
          env:
            - name: TARGET
              value: "World"
```

```
kubectl apply -f my-service.yaml
```

### 9.2 The Complete Timeline

```
TIME   EVENT                          ACTOR              RESULT
─────────────────────────────────────────────────────────────────────────────────
 T+0   kubectl apply                 kubectl            POST /apis/serving.../v1/namespaces/default/services
       API server validates,                            Service "hello" written to etcd
       sets Generation=1

 T+1   Service informer fires        Service informer   Work queue: enqueue "default/hello"
       (ADD event)

 T+2   Service reconciler dequeues   Service reconciler Read Service "hello" from lister (cache)
                                                        configurationLister.Get("hello") → NotFound
                                                        → c.createConfiguration()
                                                        Configuration "hello" created (Generation=1)
                                                        routeLister.Get("hello") → NotFound
                                                        → c.createRoute()
                                                        Route "hello" created (Generation=1)
                                                        config.Generation(1) ≠ ObservedGeneration(0)
                                                        → MarkConfigurationNotReconciled()
                                                        route.Generation(1) ≠ ObservedGeneration(0)
                                                        → MarkRouteNotReconciled()
                                                        return nil → framework UpdateStatus()

 T+3   Configuration informer fires  Config informer    Work queue: enqueue "default/hello"
       (ADD event — new Configuration)                  (FilterController matches OwnerRef=Service)
                                                        → EnqueueControllerOf → Service reconciler

       Route informer fires          Route informer     Work queue: enqueue "default/hello"
       (ADD event — new Route)                          (FilterController matches OwnerRef=Service)

       Configuration reconciler      Config reconciler  Read Configuration "hello" (Generation=1)
       dequeues "default/hello"                         latestCreatedRevision() → NotFound
                                                        → c.createRevision()
                                                        Revision "hello-00001" created
                                                        SetLatestCreatedRevisionName("hello-00001")
                                                        rc = hello-00001.Status.Ready → Unknown
                                                        (revision just created, not yet reconciled)
                                                        return nil → framework UpdateStatus()
                                                        config.Status.ObservedGeneration = 1

 T+4   Revision informer fires       Revision informer  Configuration reconciler re-enqueued
       (ADD event — "hello-00001")   (owned by Config)  via EnqueueControllerOf

       Revision reconciler           Rev reconciler     Read Revision "hello-00001"
       dequeues "default/hello-00001"                   reconcileDigest():
                                                          ContainerStatuses empty → submit to
                                                          backgroundResolver queue
                                                          return false (not yet resolved)
                                                        MarkResourcesAvailableUnknown(
                                                          "ResolvingDigests")
                                                        return nil (wait for background worker)

 T+5   Background digest worker      100-worker pool    Call registry API for
       picks up work item                               gcr.io/knative-samples/helloworld-go
                                                        Resolve tag → SHA digest
                                                        Store result in backgroundResolver map
                                                        completionCallback("default/hello-00001")
                                                        → impl.EnqueueKey("default/hello-00001")

 T+6   Revision reconciler           Rev reconciler     Read Revision "hello-00001"
       dequeues "default/hello-00001"                   reconcileDigest():
       (re-enqueued by callback)                          ContainerStatuses now populated ← digest
                                                          resolved, return true
                                                        reconcileDeployment():
                                                          Deployment "hello-00001" → NotFound
                                                          MakeDeployment() → creates Deployment
                                                          with image=gcr.io/.../helloworld-go@sha256:...
                                                          queue-proxy sidecar injected
                                                        reconcileImageCache():
                                                          ImageCache "hello-00001" created
                                                        reconcilePA():
                                                          PodAutoscaler "hello-00001" → NotFound
                                                          MakePA() → creates PA
                                                          ContainerConcurrency, ScaleTargetRef set
                                                        Deployment not yet ready → conditions Unknown
                                                        return nil → framework UpdateStatus()

 T+7   Deployment informer fires     Deployment informer Revision reconciler re-enqueued
       (ADD event — "hello-00001")   (owned by Revision)  via EnqueueControllerOf
       PA informer fires             PA informer          Revision reconciler re-enqueued

       Kubernetes scheduler          kube-scheduler     Schedule pod onto node
       Container runtime pulls       kubelet            Pull gcr.io/.../helloworld-go@sha256:...
       image, starts containers                         Start user container + queue-proxy

 T+8   Pod becomes Running/Ready     kubelet            Deployment.Status updated:
                                                        AvailableReplicas=1, ReadyReplicas=1

       Deployment informer fires     Deployment informer Work queue: enqueue "default/hello-00001"
       (UPDATE — replicas ready)     (owned by Revision)

 T+9   Revision reconciler           Rev reconciler     reconcileDeployment():
       dequeues "default/hello-00001"                     Deployment exists, checkAndUpdateDeployment()
                                                          PropagateDeploymentStatus():
                                                            Deployment.Ready=True
                                                            → RevisionConditionResourcesAvailable=True
                                                        reconcilePA():
                                                          PA exists, syncAnnotationsForKPA()
                                                          PropagateAutoscalerStatus():
                                                            PA might still be Unknown
                                                        If both ResourcesAvailable + ContainerHealthy = True
                                                          → RevisionConditionReady = True
                                                        readyAfterReconcile=True, readyBeforeReconcile=False
                                                        → emit "RevisionReady" event
                                                        return nil → framework UpdateStatus()

 T+10  Revision informer fires       Revision informer  Config reconciler re-enqueued:
       (UPDATE — "hello-00001" Ready) (owned by Config)  EnqueueControllerOf → Configuration "hello"

 T+11  Configuration reconciler      Config reconciler  Read Configuration "hello"
       dequeues "default/hello"                         latestCreatedRevision() → "hello-00001" (exists)
                                                        SetLatestCreatedRevisionName("hello-00001")
                                                        rc = hello-00001.Status.Ready → True
                                                        findAndSetLatestReadyRevision():
                                                          "hello-00001".Ready=True
                                                          → SetLatestReadyRevisionName("hello-00001")
                                                          → config.Status.MarkReady()
                                                        return nil → framework UpdateStatus()
                                                        config.Status.ObservedGeneration=1

 T+12  Configuration informer fires  Config informer    Service reconciler re-enqueued:
       (UPDATE — config Ready)        (owned by Service) EnqueueControllerOf → Service "hello"

 T+13  Service reconciler            Service reconciler Read Service "hello"
       dequeues "default/hello"                         configurationLister.Get("hello") → found
                                                        config.Generation(1)==ObservedGeneration(1) ✓
                                                        PropagateConfigurationStatus():
                                                          LatestReadyRevisionName="hello-00001"
                                                          ConfigurationsReady=True
                                                        CheckNameAvailability() → ok
                                                        routeLister.Get("hello") → found
                                                        route.Generation(1) ≠ ObservedGeneration(0)
                                                        → MarkRouteNotReconciled()
                                                        return nil → framework UpdateStatus()

       Meanwhile: Route reconciler   Route reconciler   Route "hello" was enqueued at T+3
       dequeues "default/hello"      (ran in parallel)  Generation(1) ≠ ObservedGeneration(0)
                                                        → MarkIngressNotConfigured()
                                                        configureTraffic():
                                                          Traffic[0].ConfigurationName = "hello"
                                                          Resolve "hello" → LatestReadyRevisionName
                                                          = "hello-00001" (now ready)
                                                          tracker.Track(Configuration "hello")
                                                          tracker.Track(Revision "hello-00001")
                                                          AllTrafficAssigned=True
                                                        Set Address = http://hello.default.svc.cluster.local
                                                        reconcilePlaceholderServices():
                                                          k8s Service "hello" created (no selector)
                                                          k8s Service for each tag created
                                                        externalDomainTLS() → TLS disabled or no cert
                                                        reconcileIngress():
                                                          Ingress "hello" → NotFound → create
                                                          Rules: hello.default.example.com →
                                                            ingress backend → k8s Service "hello"
                                                          Backends point at → Revision "hello-00001"
                                                        ingress.Generation(1) ≠ ObservedGeneration(0)
                                                        → MarkIngressNotConfigured()
                                                        updatePlaceholderServices()
                                                        return nil → framework UpdateStatus()

 T+14  Ingress controller            Kourier/Istio/     Watch Ingress "hello" (ADD event)
       (e.g. Kourier)                Contour            Program Envoy/proxy with routes:
                                                          hello.default.example.com/* →
                                                          Pod IP of "hello-00001" pod(s)
                                                        Update Ingress.Status.Conditions:
                                                          NetworkConfigured=True → Ready=True

 T+15  Ingress informer fires        Ingress informer   Route reconciler re-enqueued:
       (UPDATE — Ingress Ready)      (owned by Route)   EnqueueControllerOf → Route "hello"

 T+16  Route reconciler              Route reconciler   ingress.Generation==ObservedGeneration ✓
       dequeues "default/hello"                         !roInProgress (no rollout)
                                                        PropagateIngressStatus():
                                                          Ingress.Ready=True
                                                          → RouteConditionIngressReady=True
                                                          → RouteConditionAllTrafficAssigned=True
                                                          → Route.Status.Ready=True
                                                          → Route.Status.URL = "hello.default.example.com"
                                                          → Route.Status.Traffic[0]:
                                                              RevisionName="hello-00001", Percent=100
                                                        return nil → framework UpdateStatus()

 T+17  Route informer fires          Route informer     Service reconciler re-enqueued:
       (UPDATE — Route Ready)        (owned by Service) EnqueueControllerOf → Service "hello"

 T+18  Service reconciler            Service reconciler config.Generation==ObservedGeneration ✓
       dequeues "default/hello"                         PropagateConfigurationStatus() → ConfigsReady=True
                                                        route.Generation==ObservedGeneration ✓
                                                        PropagateRouteStatus():
                                                          Route.Ready=True
                                                          → RoutesReady=True
                                                          → Service.Status.URL = "hello.default.example.com"
                                                        checkRoutesNotReady():
                                                          want[0]: RevisionName="hello-00001" Percent=100
                                                          got[0]:  RevisionName="hello-00001" Percent=100
                                                          diff = "" → no action
                                                        Both ConfigsReady + RoutesReady = True
                                                        → Service.Status.Ready = True
                                                        return nil → framework UpdateStatus()

 T+19  STEADY STATE: Traffic is being served
       curl https://hello.default.example.com → "Hello World!"
```

### 9.3 ASCII Architecture Diagram

```
                              kubectl apply
                                    │
                                    ▼
                          ┌─────────────────┐
                          │   API Server    │
                          │  etcd: Service  │
                          │  hello Gen=1    │
                          └────────┬────────┘
                                   │ Watch (ADD)
                          ┌────────▼────────┐
                          │ Service Informer │
                          │   Work Queue     │
                          └────────┬────────┘
                                   │
                          ┌────────▼───────────────────────────────────────┐
                          │          SERVICE RECONCILER                     │
                          │  1. configurationLister.Get → NotFound          │
                          │  2. createConfiguration() ──────────────────►  │
                          │  3. routeLister.Get → NotFound                  │
                          │  4. createRoute() ───────────────────────────►  │
                          └───────────────┬────────────────┬────────────────┘
                                          │                │
                          ┌───────────────▼───┐    ┌───────▼──────────────┐
                          │  Configuration    │    │       Route          │
                          │  "hello" Gen=1    │    │   "hello" Gen=1      │
                          └─────────┬─────────┘    └──────────┬───────────┘
                                    │ Watch (ADD)              │ Watch (ADD)
                          ┌─────────▼─────────┐    ┌──────────▼───────────┐
                          │  Config Reconciler │    │   Route Reconciler   │
                          │  createRevision()  │    │  configureTraffic()  │
                          └─────────┬──────────┘   │  reconcileIngress()  │
                                    │               └──────────┬───────────┘
                          ┌─────────▼─────────┐               │
                          │  Revision         │    ┌──────────▼───────────┐
                          │  "hello-00001"    │    │  Knative Ingress     │
                          │  Gen=1            │    │  "hello"             │
                          └─────────┬─────────┘    └──────────┬───────────┘
                                    │ Watch (ADD)              │ Watch (ADD)
                          ┌─────────▼──────────┐   ┌──────────▼───────────┐
                          │  Rev Reconciler    │   │  Ingress Controller  │
                          │  reconcileDigest() │   │  (Kourier/Istio)     │
                          │  ─── blocks ───    │   │  Programs Envoy      │
                          │  bgResolver queue  │   └──────────┬───────────┘
                          └──────┬──────┬──────┘             │ Ingress Ready
                                 │      │                     │
                   100 workers   │      │           ┌─────────▼─────────────┐
                          ┌──────▼──┐   │           │  Route Reconciler     │
                          │ Registry│   │           │  PropagateIngress     │
                          │  HTTP   │   │           │  Status()             │
                          └──────┬──┘   │           │  Route.Ready = True   │
                    digest        │      │           └─────────┬─────────────┘
                    resolved      │      │                     │ Route Watch (UPDATE)
                          ┌───────▼──┐   │                     │
                          │EnqueueKey│   │           ┌─────────▼─────────────┐
                          └───────┬──┘   │           │  Service Reconciler   │
                                  │      │           │  PropagateRouteStatus │
                          ┌───────▼──────▼───────┐   │  Service.Ready = True │
                          │  Rev Reconciler       │   └───────────────────────┘
                          │  reconcileDeployment()│
                          │  reconcilePA()        │
                          └─────────┬─────────────┘
                                    │
                          ┌─────────▼─────────┐
                          │  Deployment       │
                          │  "hello-00001"    │
                          └─────────┬─────────┘
                                    │  Pod Scheduled + Running
                          ┌─────────▼──────────┐
                          │  Rev Reconciler    │
                          │  PropagateDeployment│
                          │  Status()          │
                          │  Rev.Ready = True  │
                          └─────────┬──────────┘
                                    │ Revision Watch (UPDATE)
                          ┌─────────▼──────────┐
                          │  Config Reconciler  │
                          │  SetLatestReady     │
                          │  RevisionName()     │
                          │  Config.Ready = True│
                          └─────────┬───────────┘
                                    │ Config Watch (UPDATE)
                          ┌─────────▼──────────────┐
                          │   Service Reconciler    │
                          │   PropagateConfig       │
                          │   Status()              │
                          └────────────────────────-┘
```

### 9.4 The Seven Enqueue Triggers

The reconciliation above is driven by exactly seven distinct enqueue events. Understanding each one shows how the system is wired together:

| # | Trigger | Work Queue Entry | Reconciler |
|---|---------|-----------------|------------|
| 1 | Service ADD (kubectl apply) | `default/hello` | Service |
| 2 | Configuration ADD (created by Service reconciler) | `default/hello` | Configuration; also re-enqueues Service via FilterController |
| 3 | Route ADD (created by Service reconciler) | `default/hello` | Route; also re-enqueues Service via FilterController |
| 4 | Revision ADD (created by Config reconciler) | `default/hello-00001` | Revision; also re-enqueues Configuration via FilterController |
| 5 | `impl.EnqueueKey` callback from background digest resolver | `default/hello-00001` | Revision |
| 6 | Deployment UPDATE (pods became ready) | `default/hello-00001` | Revision via FilterController |
| 7 | Ingress UPDATE (programmed by Kourier/Istio) | `default/hello` | Route via FilterController |

Every other update in the chain (Configuration→Service, Revision→Configuration, Route→Service) is driven by the same FilterController+EnqueueControllerOf pattern: when a child resource changes, its OwnerReference is inspected and the parent is enqueued.

### 9.5 Generation Guards in Practice

Two generation guards are visible in the walkthrough:

**Guard 1 — Service reconciler, Configuration path** ([service.go:83](pkg/reconciler/service/service.go#L83)):
```go
if config.Generation != config.Status.ObservedGeneration {
    service.Status.MarkConfigurationNotReconciled()
    if config.Spec.GetTemplate().Name != "" {
        return nil  // BYO-Name: must serialize
    }
}
```
At T+2 this fires: Configuration was just created (Generation=1) but Config reconciler hasn't run yet (ObservedGeneration=0). The Service marks itself as waiting but does not block — it proceeds to create the Route too. This is correct because the Service reconciler will be re-enqueued when the Configuration finishes reconciling.

**Guard 2 — Route reconciler, Ingress path** ([route.go:171](pkg/reconciler/route/route.go#L171)):
```go
if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration {
    r.Status.MarkIngressNotConfigured()
} else if !roInProgress {
    r.Status.PropagateIngressStatus(ingress.Status)
}
```
At T+13 this fires: the Ingress was just created (Generation=1) but the Ingress controller hasn't programmed it yet (ObservedGeneration=0). The Route waits. At T+16 the Ingress controller has updated Ingress.Status and bumped ObservedGeneration, so `PropagateIngressStatus` runs and the Route becomes Ready.

The generation guard is why the system is safe against false-ready signals: a controller only trusts a child resource's conditions if `ObservedGeneration == Generation`, meaning that child's own reconciler has processed the current spec.

### 9.6 Status Propagation Chain

The full status propagation flows strictly bottom-up:

```
Pod (kubelet)
  │  Running/Ready
  ▼
Deployment.Status.AvailableReplicas
  │  PropagateDeploymentStatus()
  ▼
Revision.Status.Conditions[ResourcesAvailable=True]
Revision.Status.Conditions[ContainerHealthy=True]
  │  (cascade via ConditionSet)
  ▼
Revision.Status.Conditions[Ready=True]
  │  findAndSetLatestReadyRevision()
  ▼
Configuration.Status.LatestReadyRevisionName = "hello-00001"
Configuration.Status.Conditions[Ready=True]
  │  PropagateConfigurationStatus()
  ▼
Service.Status.LatestReadyRevisionName = "hello-00001"
Service.Status.Conditions[ConfigurationsReady=True]

Ingress (programmed by Kourier/Istio)
  │  PropagateIngressStatus()
  ▼
Route.Status.Conditions[IngressReady=True]
Route.Status.Conditions[AllTrafficAssigned=True]
  │  (cascade via ConditionSet)
  ▼
Route.Status.Conditions[Ready=True]
Route.Status.URL = "hello.default.example.com"
Route.Status.Traffic[0].RevisionName = "hello-00001"
  │  PropagateRouteStatus()
  ▼
Service.Status.Conditions[RoutesReady=True]
Service.Status.URL = "hello.default.example.com"
  │  (both ConfigurationsReady + RoutesReady = True)
  ▼
Service.Status.Conditions[Ready=True]
```

Each step is triggered by a Watch event on the child resource, not by polling. The chain has no timeouts or sleeps — it advances exactly as fast as each reconciler can process events.

### 9.7 What Happens on `kubectl apply` Again (Update Path)

When you edit the Service (e.g., change the image):

1. API server increments `Service.Generation` to 2.
2. Service reconciler runs: `reconcileConfiguration()` sees `desiredConfig.Spec != existing.Spec` (semantic diff), calls `Update()`. Configuration.Generation becomes 2.
3. Configuration reconciler runs: `latestCreatedRevision()` queries revisions with generation label `serving.knative.dev/configurationGeneration=2` — finds none, calls `createRevision()`. Revision "hello-00002" created.
4. Revision reconciler processes "hello-00002": resolves digest, creates Deployment with new image, waits for pod.
5. Once "hello-00002" pod is Running, `RevisionConditionReady=True` propagates up: Configuration sets `LatestReadyRevisionName="hello-00002"`.
6. Route reconciler runs: `configureTraffic()` now resolves the configuration name to `"hello-00002"`. Ingress is updated with new backend. Old pod ("hello-00001") remains running until traffic shifts.
7. GC reconciler (separate) eventually cleans up old Revisions beyond the `revisionHistoryLimit`.

The old revision is **never deleted synchronously** — it stays alive until the GC reconciler determines it is safe to remove. This is what enables instant rollback: `kubectl` can update the Route's traffic split to point back at "hello-00001" with zero downtime.

### 9.8 Failure Modes and Self-Healing

The level-triggered design means the system recovers from every failure class automatically:

| Failure | Effect | Recovery |
|---------|--------|----------|
| Service reconciler crashes mid-run | Service.Status may be stale | Next reconcile (re-queued by rate limiter) re-reads everything and recomputes |
| `createConfiguration` returns 409 Conflict | Returns error, item re-queued | Next reconcile: `configurationLister.Get` finds the existing one, proceeds normally |
| Registry unreachable during digest resolution | `backgroundResolver` stores error | Revision reconciler emits error event; item re-queued with exponential backoff |
| Ingress controller restarts | Ingress.Status goes stale | Route reconciler sees `Generation≠ObservedGeneration`, marks IngressNotConfigured; ingress controller re-programs on restart |
| etcd write fails on `UpdateStatus` | Framework logs error, re-queues | Next reconcile re-reads, re-computes, re-attempts write |
| Node running pod dies | Deployment controller replaces pod | New pod → Deployment.AvailableReplicas recovers → PropagateDeploymentStatus re-fires |

No special recovery code exists anywhere. Every failure is handled by the same mechanism: re-queue, re-read, re-compute.

---

---

## 10. Ownership, Generations & Idempotency

This section explains the three guarantees that make the reconciliation system correct: **ownership** (who may manage what), **generation tracking** (how staleness is detected), and **idempotency** (why running the same reconciler twice is always safe).

### 10.1 The Ownership Model

Every resource in the hierarchy has exactly one owner. Kubernetes encodes this relationship in `metadata.ownerReferences`. When the owner is deleted, Kubernetes garbage-collects all owned resources via **cascading deletion** — no controller code is needed for cleanup.

#### 10.1.1 OwnerReference Structure

An `OwnerReference` has five fields:

```go
// vendor/knative.dev/pkg/kmeta/owner_references.go:36
func NewControllerRef(obj OwnerRefable) *metav1.OwnerReference {
    return metav1.NewControllerRef(obj.GetObjectMeta(), obj.GetGroupVersionKind())
}
```

This produces:
```yaml
ownerReferences:
  - apiVersion: serving.knative.dev/v1
    kind: Service
    name: hello
    uid: <service-uid>
    controller: true          # ← exactly one owner may have controller=true
    blockOwnerDeletion: true  # ← deletion of Service blocks until this child is GC'd
```

The two critical fields:
- `controller: true` — marks the _controlling_ owner. `metav1.IsControlledBy(child, owner)` checks that the child's controller OwnerReference matches the candidate owner's UID. A resource may have multiple OwnerReferences (e.g., for non-controller owners), but only one may be the controller.
- `blockOwnerDeletion: true` — tells the Kubernetes garbage collector to wait for this object to be deleted before finishing deletion of its owner. This ensures ordered teardown.

#### 10.1.2 The Ownership Tree

```
Service "hello"  (uid: aaa)
├── Configuration "hello"    ownerRef: Service aaa, controller=true
│   └── Revision "hello-00001"  ownerRef: Configuration bbb, controller=true
│       ├── Deployment "hello-00001"   ownerRef: Revision ccc, controller=true
│       │   └── ReplicaSet / Pods     (owned by Deployment — standard k8s)
│       ├── PodAutoscaler "hello-00001" ownerRef: Revision ccc, controller=true
│       └── ImageCache "hello-00001"   ownerRef: Revision ccc, controller=true
└── Route "hello"            ownerRef: Service aaa, controller=true
    ├── k8s Service "hello"  ownerRef: Route ddd, controller=true
    └── Ingress "hello"      ownerRef: Route ddd, controller=true
        └── Certificate "hello" (if TLS)  ownerRef: Route ddd, controller=true
```

**Cascade deletion example**: `kubectl delete ksvc hello`
1. API server marks Service "hello" with a `DeletionTimestamp`.
2. Kubernetes GC sees that Configuration "hello" and Route "hello" have `blockOwnerDeletion=true` pointing to Service "hello".
3. GC deletes Configuration "hello" → triggers deletion of Revision "hello-00001" → triggers deletion of Deployment, PA, ImageCache → triggers deletion of Pods.
4. GC deletes Route "hello" → triggers deletion of Ingress, k8s Services.
5. Once all owned objects are gone, the Service finalizer (if any) is removed and the Service object itself is deleted.

No controller writes any delete call during this cascade. Kubernetes handles it entirely through OwnerReference GC.

#### 10.1.3 Ownership Verification

Every reconciler that reads a child resource it might have created also verifies ownership before touching it ([service.go:142](pkg/reconciler/service/service.go#L142)):

```go
} else if !metav1.IsControlledBy(config, service) {
    service.Status.MarkConfigurationNotOwned(configName)
    return nil, fmt.Errorf("service: %q does not own configuration: %q",
        service.Name, configName)
}
```

The same check appears for Route ([service.go:165](pkg/reconciler/service/service.go#L165)) and for Revision ownership by Configuration ([service.go:308](pkg/reconciler/service/service.go#L308)):

```go
if !metav1.IsControlledBy(rev, config) {
    return errConflict
}
```

This prevents a reconciler from accidentally clobbering a same-named resource that belongs to a different parent — a scenario that can happen if two Services share a name (impossible in the same namespace, but a valid guard at the code level), or if a user manually created a resource with the same name before deploying the Service.

The surface error is reflected in the Service's status conditions so users see a clear message rather than a cryptic API error.

### 10.2 Generation Tracking

Kubernetes increments `metadata.generation` every time the resource's `spec` changes. The reconciler sets `status.observedGeneration` at the end of each successful reconcile pass (the generated framework does this automatically). The gap `generation - observedGeneration` is the number of spec changes not yet processed.

#### 10.2.1 The IsReady Contract

Every Knative resource defines `IsReady()` with the same two-part contract ([revision_lifecycle.go:72](pkg/apis/serving/v1/revision_lifecycle.go#L72)):

```go
func (r *Revision) IsReady() bool {
    rs := r.Status
    return rs.ObservedGeneration == r.Generation &&
        rs.GetCondition(RevisionConditionReady).IsTrue()
}
```

The same pattern appears in all four types:

```go
// service_lifecycle.go:51
return ss.ObservedGeneration == s.Generation && ...IsTrue()

// configuration_lifecycle.go:41
return cs.ObservedGeneration == c.Generation && ...IsTrue()

// route_lifecycle.go:51
return rs.ObservedGeneration == r.Generation && ...IsTrue()
```

**Why both conditions?** `Ready=True` with a stale `ObservedGeneration` is a lie: the conditions were computed against an older spec. Any parent that trusts `Ready=True` from a child that has a pending spec change may advance prematurely. The `ObservedGeneration == Generation` check is a staleness guard that makes the `IsReady()` contract mean "this object's _current_ spec has been fully reconciled and the result is Ready."

#### 10.2.2 Where Generation Guards Fire in the Chain

| Guard Location | What it catches | Code reference |
|---|---|---|
| Service reconciler checks Configuration | Config was just created or updated, its reconciler hasn't run yet | [service.go:83](pkg/reconciler/service/service.go#L83) |
| Service reconciler checks Route | Route was just created or updated, its reconciler hasn't run yet | [service.go:116](pkg/reconciler/service/service.go#L116) |
| Route reconciler checks Ingress | Ingress was just created or updated, ingress controller hasn't programmed it yet | [route.go:171](pkg/reconciler/route/route.go#L171) |

In each case the reconciler marks a `NotReconciled` or `NotConfigured` condition on itself (Unknown, not False) and returns `nil`. Returning `nil` tells the framework "no error — don't retry". The system will advance when the child finishes reconciling and its informer fires an UPDATE event that re-enqueues the parent.

#### 10.2.3 The BYO-Name Serialization Case

The BYO-Name path (user sets `spec.template.name` explicitly) requires stricter sequencing ([service.go:89](pkg/reconciler/service/service.go#L89)):

```go
if config.Generation != config.Status.ObservedGeneration {
    service.Status.MarkConfigurationNotReconciled()
    if config.Spec.GetTemplate().Name != "" {
        return nil  // ← hard stop: do NOT create Route yet
    }
}
```

Normally (auto-name), the Service reconciler creates both Configuration and Route in the same pass even though Configuration hasn't reconciled yet. This is safe because the Route uses `ConfigurationName` in its traffic target — it doesn't need to know which Revision will be created.

But with BYO-Name, the Route needs to reference the specific revision name. If the Service reconciler programs the Route before Configuration has created that revision, the Route will reference a non-existent revision name. So the Service blocks Route creation until `config.Generation == config.Status.ObservedGeneration`.

### 10.3 Idempotency Guarantees

The reconciliation loop is designed so that running any reconciler any number of times produces the same result. This section explains exactly how each operation achieves idempotency.

#### 10.3.1 Get-or-Create Pattern

Every child resource creation follows the same pattern ([service.go:132](pkg/reconciler/service/service.go#L132)):

```go
config, err := c.configurationLister.Configurations(service.Namespace).Get(configName)
if apierrs.IsNotFound(err) {
    config, err = c.createConfiguration(ctx, service)
    // handle error
} else if err != nil {
    return nil, fmt.Errorf("failed to get Configuration: %w", err)
} else if !metav1.IsControlledBy(config, service) {
    // ownership error
} else if config, err = c.reconcileConfiguration(ctx, service, config); err != nil {
    // reconcile existing
}
```

The structure is: **try lister first** (O(1) cache read), and only call the API if not found. If the API call itself returns `AlreadyExists` (race between two reconciler workers for the same key), the error is returned and the item is re-queued — on the next pass, `configurationLister.Get` will find the object (the informer cache will have been updated by the time the item is reprocessed).

#### 10.3.2 Semantic Diff Before Update

The update path for both Configuration ([service.go:207](pkg/reconciler/service/service.go#L207)) and Route ([service.go:253](pkg/reconciler/service/service.go#L253)) performs a **semantic diff** before calling `Update`:

```go
func configSemanticEquals(ctx context.Context, desiredConfig, config *v1.Configuration) (bool, error) {
    specDiff, err := kmp.SafeDiff(desiredConfig.Spec, config.Spec)
    // ...
    return equality.Semantic.DeepEqual(desiredConfig.Spec, config.Spec) &&
        equality.Semantic.DeepEqual(desiredConfig.Labels, config.Labels) &&
        equality.Semantic.DeepEqual(desiredConfig.Annotations, config.Annotations) &&
        specDiff == "", nil
}
```

`equality.Semantic.DeepEqual` from `k8s.io/apimachinery` treats semantically equivalent values as equal — e.g., a nil slice and an empty slice are equal. `kmp.SafeDiff` is used in parallel to generate a human-readable diff for logging.

Critically, before the diff runs, defaults are applied to the _existing_ object ([service.go:227](pkg/reconciler/service/service.go#L227)):

```go
existing := config.DeepCopy()
existing.SetDefaults(ctx)   // ← apply current defaults to existing before comparing
desiredConfig := resources.MakeConfigurationFromExisting(service, existing)
equals, err := configSemanticEquals(ctx, desiredConfig, existing)
```

**Why**: After a Knative upgrade, new default values may be added to the webhook defaulting logic. Without `SetDefaults` before the diff, every existing resource would appear to differ from its desired state — triggering an unnecessary `Update` on every reconcile of every resource in the cluster. The `SetDefaults` call normalizes the existing object to what the _current_ defaulting logic would produce, so the diff only captures real spec changes.

#### 10.3.3 Immutable Revision Specs

Revisions are **never updated**. Their spec is sealed at creation time ([configuration.go:299](pkg/reconciler/configuration/configuration.go#L299)):

```go
func (c *Reconciler) createRevision(ctx context.Context, config *v1.Configuration) (*v1.Revision, error) {
    rev, err := resources.MakeRevision(ctx, config, c.clock)
    // ...
    return c.client.ServingV1().Revisions(config.Namespace).Create(ctx, rev, metav1.CreateOptions{})
}
```

There is no `reconcileRevision` or `updateRevision`. If the spec changes (via a new Service spec), the Configuration reconciler creates a **new** Revision with the next generation in its name (`hello-00002`). The old Revision stays unchanged.

This immutability is what makes rollback trivial: the old Revision's Deployment still exists (until GC), and pointing a Route traffic target back at the old Revision name instantly restores the previous behavior with no reconciler work.

#### 10.3.4 Mutable Fields Only

When the Service reconciler updates a Configuration or Route, it only touches the fields it owns ([service.go:241](pkg/reconciler/service/service.go#L241)):

```go
// Preserve the rest of the object (e.g. ObjectMeta except for labels).
existing.Spec = desiredConfig.Spec
existing.Labels = desiredConfig.Labels
existing.Annotations = desiredConfig.Annotations
return c.client.ServingV1().Configurations(service.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
```

It uses `existing` (the live object from the API) as the base, only overwriting `Spec`, `Labels`, and `Annotations`. This preserves `Status`, `Finalizers`, `ResourceVersion` (required for optimistic concurrency), and any other metadata that other controllers may have written. It does NOT call `UpdateStatus` — status updates go through a separate subresource.

### 10.4 The GC Reconciler

The garbage collector ([pkg/reconciler/gc/](pkg/reconciler/gc/)) is a separate controller that runs `ReconcileKind` on **Configuration** objects. It does not own any resources — it only deletes Revisions that are no longer needed.

#### 10.4.1 What Makes a Revision Eligible for GC

A Revision is eligible for deletion only when **all** of the following are true ([gc.go:135](pkg/reconciler/gc/gc.go#L135)):

```go
func isRevisionActive(rev *v1.Revision, config *v1.Configuration) bool {
    if config.Status.LatestReadyRevisionName == rev.Name {
        return true  // never delete the latest ready revision
    }
    if strings.EqualFold(rev.Annotations[serving.RevisionPreservedAnnotationKey], "true") {
        return true  // user opted out of GC with serving.knative.dev/no-gc=true
    }
    // Anything not explicitly labelled "reserve" by the labeler is kept
    return rev.GetRoutingState() != v1.RoutingStateReserve
}
```

A Revision is **active** (not eligible) if:
1. It is `LatestReadyRevisionName` — the fallback for all traffic, always kept.
2. It has the `serving.knative.dev/no-gc: "true"` annotation — user-opted-out.
3. Its routing state is not `reserve` — `pending` and `active` Revisions are live.

Only Revisions with `RoutingState=reserve` (set by the labeler reconciler after they leave the Route's traffic targets) are candidates for deletion.

#### 10.4.2 Staleness Criteria

Even a `reserve` Revision is not deleted immediately. It must also pass the staleness check ([gc.go:148](pkg/reconciler/gc/gc.go#L148)):

```go
func isRevisionStale(cfg *gc.Config, rev *v1.Revision, logger *zap.SugaredLogger) bool {
    createTime := rev.ObjectMeta.CreationTimestamp.Time
    if sinceCreate != gc.Disabled && time.Since(createTime) < sinceCreate {
        return false  // too recently created
    }
    active := revisionLastActiveTime(rev)
    if sinceActive != gc.Disabled && time.Since(active) < sinceActive {
        return false  // too recently active
    }
    return true
}
```

Default config ([gc/config.go:61](pkg/gc/config.go#L61)):

```go
func defaultConfig() *Config {
    return &Config{
        RetainSinceCreateTime:     48 * time.Hour,
        RetainSinceLastActiveTime: 15 * time.Hour,
        MinNonActiveRevisions:     20,
        MaxNonActiveRevisions:     1000,
    }
}
```

| Parameter | Default | Meaning |
|-----------|---------|---------|
| `retain-since-create-time` | 48h | Keep any Revision created within the last 48 hours |
| `retain-since-last-active-time` | 15h | Keep any Revision that was last routed-to within 15 hours |
| `min-non-active-revisions` | 20 | Always keep at least 20 non-active Revisions regardless of age |
| `max-non-active-revisions` | 1000 | Hard cap: delete oldest if count exceeds 1000 |

The GC runs in two passes: first delete stale revisions, then if `nonStaleCount > max`, delete the oldest non-active ones until under the cap.

#### 10.4.3 The RoutingState Label Lifecycle

The three-state machine on every Revision ([revision_helpers.go:50](pkg/apis/serving/v1/revision_helpers.go#L50)):

```
  created                routed-to              removed from route
    │                       │                          │
    ▼                       ▼                          ▼
┌─────────┐           ┌──────────┐             ┌──────────────┐
│ pending │──────────►│  active  │────────────►│   reserve    │──► GC eligible
└─────────┘           └──────────┘             └──────────────┘
```

- **pending**: Set by `resources.MakeRevision` at creation time. Treated as active for GC purposes. The Revision is "probably about to be routed to."
- **active**: Set by the labeler reconciler when the Route's traffic targets include this Revision. The PodAutoscaler's `Reachability=Reachable` is set based on this.
- **reserve**: Set by the labeler reconciler when the Route's traffic targets no longer include this Revision. The PA's `Reachability=Unreachable` causes scale-to-zero. GC may delete after staleness thresholds pass.

The `routingStateModified` annotation records the timestamp of the last state transition. GC uses `revisionLastActiveTime()` which returns this timestamp (or creation time if never set) to calculate `retain-since-last-active-time`.

### 10.5 Putting It All Together: The Invariants

The system maintains three invariants at all times:

#### Invariant 1: A resource is Ready only when its current spec has been observed

```
IsReady() ⟺ ObservedGeneration == Generation AND Ready condition = True
```

This means: a parent's `PropagateXStatus()` call only advances the parent's conditions when the child has processed its current spec. Stale `Ready=True` from a previous generation is ignored by the guard.

#### Invariant 2: Every spec change to a Configuration produces a new immutable Revision

Configuration reconciler never updates an existing Revision. It creates a new one only when `latestCreatedRevision` returns NotFound for the current generation. The generation label `serving.knative.dev/configurationGeneration` enables O(1) lookup of "the revision for this configuration generation."

```
Config.Generation=N → Revision label configurationGeneration=N
```

If multiple concurrent reconcile runs attempt to create the same Revision, the second gets `AlreadyExists`, returns an error, and re-queues. On re-processing, `latestCreatedRevision` finds the existing Revision and proceeds normally.

#### Invariant 3: A controller only modifies resources it owns

Every reconciler verifies `metav1.IsControlledBy(child, parent)` before updating or deleting a child resource. This ensures that if a user creates a resource with the same name as one that would be auto-created, the controller surfaces a clear error instead of silently clobbering user data.

The verification uses the **UID**, not just the name:

```go
// metav1.IsControlledBy checks:
// child.OwnerReferences[i].Controller == true
//   AND child.OwnerReferences[i].UID == owner.UID
```

Name collisions within a namespace are impossible (the API server rejects duplicates), but the UID check protects against the edge case where a resource was deleted and recreated with the same name — the new object has a different UID and a different controller.

### 10.6 Annotated Resource Lifecycle

A complete lifecycle of a single Revision from creation to deletion, annotated with which invariant each step enforces:

```
CONFIG RECONCILER creates "hello-00001"
  Label: configurationGeneration=1          ← enables O(1) lookup (Invariant 2)
  Label: routingState=pending               ← not GC-eligible yet (Invariant 3 via GC)
  OwnerRef: Configuration "hello" uid=bbb   ← cascade delete (Invariant 3)

REVISION RECONCILER resolves digest, creates Deployment
  Deployment OwnerRef: Revision uid=ccc     ← cascade delete

POD starts → Deployment.AvailableReplicas=1
  PropagateDeploymentStatus → Rev.Ready=True, ObservedGeneration=1
                                             ← Invariant 1 satisfied for Revision

CONFIG RECONCILER: SetLatestReadyRevisionName("hello-00001")
  Config.Ready=True, ObservedGeneration=1   ← Invariant 1 satisfied for Config

LABELER RECONCILER: Route references "hello-00001"
  Label: routingState=active                ← Revision protected from GC

--- user deploys new version ---

CONFIG RECONCILER creates "hello-00002"
  "hello-00001" still exists, still active

LABELER RECONCILER: Route now references only "hello-00002"
  "hello-00001" Label: routingState=reserve ← GC eligible after retain period
  PA Reachability=Unreachable               ← scale-to-zero initiated

GC RECONCILER (runs on Configuration "hello")
  "hello-00001" is reserve, age > 48h, lastActive > 15h ago
  → client.ServingV1().Revisions(...).Delete("hello-00001")
  Kubernetes GC: Deployment, PA, ImageCache cascade deleted
```

---

_This completes core.md — all 10 sections written._
