# Knative Serving Controllers Documentation

This document provides a comprehensive overview of all controllers in the Knative Serving repository and their responsibilities.

## Overview

Knative Serving uses a set of reconciliation controllers that watch Kubernetes resources and ensure the actual state matches the desired state. Each controller implements the `Reconciler` interface from `knative.dev/pkg/controller` and follows the standard Kubernetes controller pattern.

All controllers are located in `pkg/reconciler/` and expose a `NewController` function that:
1. Constructs the Reconciler
2. Constructs a `controller.Impl` with that Reconciler
3. Wires up informers to enqueue events appropriately

---

## Core Serving Controllers

### 1. Service Controller

**Location:** `pkg/reconciler/service/`

**Watched Resource:** `serving.knative.dev/v1.Service`

**Responsibility:**
The Service controller is the top-level orchestrator for Knative Services. It provides a simplified interface for deploying serverless applications by managing the underlying Configuration and Route resources.

**Key Functions:**
- Creates and reconciles a **Configuration** resource based on the Service's `spec.template`
- Creates and reconciles a **Route** resource to handle traffic splitting
- Propagates status from Configuration (revision readiness) and Route (URL, traffic) back to the Service
- Handles BYO (Bring Your Own) revision names by serializing Configuration and Route reconciliation
- Validates that the Service owns its Configuration and Route

**Informers Watched:**
- Service (primary)
- Configuration (owned child)
- Route (owned child)
- Revision (for name availability checks)

---

### 2. Configuration Controller

**Location:** `pkg/reconciler/configuration/`

**Watched Resource:** `serving.knative.dev/v1.Configuration`

**Responsibility:**
The Configuration controller manages the lifecycle of Revisions. Each time the Configuration's template changes, a new Revision is created representing that snapshot.

**Key Functions:**
- Creates new **Revision** resources when the Configuration spec changes
- Tracks `LatestCreatedRevisionName` - the most recently created Revision
- Tracks `LatestReadyRevisionName` - the most recent Revision that became ready
- Handles BYO revision naming (when `spec.template.name` is set)
- Emits events when revisions are created or fail
- Manages revision generation labels for tracking

**Informers Watched:**
- Configuration (primary)
- Revision (owned children, filtered by Configuration controller)

---

### 3. Revision Controller

**Location:** `pkg/reconciler/revision/`

**Watched Resource:** `serving.knative.dev/v1.Revision`

**Responsibility:**
The Revision controller is responsible for making a Revision "ready" by creating all the necessary underlying Kubernetes resources to serve traffic.

**Key Functions:**
- **Image Digest Resolution:** Resolves container image tags to immutable digests using a background resolver with parallel workers
- **Deployment Creation:** Creates/reconciles the Kubernetes Deployment that runs the user's containers
- **PodAutoscaler Creation:** Creates the PA resource that manages autoscaling
- **Image Caching:** Creates Image (caching) resources for pre-pulling container images
- **TLS Certificate Management:** Creates queue-proxy certificates when system-internal-tls is enabled
- Updates revision status based on deployment readiness

**Informers Watched:**
- Revision (primary)
- Deployment (owned child)
- PodAutoscaler (owned child)
- Certificate (tracked for TLS)
- Image (for caching, not watched for changes)

---

### 4. Route Controller

**Location:** `pkg/reconciler/route/`

**Watched Resource:** `serving.knative.dev/v1.Route`

**Responsibility:**
The Route controller manages traffic routing to Revisions. It creates the networking infrastructure needed to expose Revisions via URLs.

**Key Functions:**
- **Traffic Configuration:** Builds traffic configuration from Route spec, resolving Configuration references to specific Revisions
- **Ingress Management:** Creates/reconciles Knative Ingress resources for the configured ingress class
- **Placeholder Services:** Creates K8s Services as stable endpoints for each traffic target
- **TLS/Certificate Management:**
    - Manages external domain TLS certificates
    - Manages cluster-local domain TLS certificates
    - Handles ACME HTTP01 challenges
    - Supports wildcard certificate matching
- **Domain Management:** Computes URLs based on domain templates and visibility settings
- **Rollout Management:** Supports gradual traffic rollouts between revisions
- Tracks Configurations and Revisions referenced in traffic spec

**Informers Watched:**
- Route (primary)
- Configuration (tracked for traffic targets)
- Revision (tracked for traffic targets)
- Ingress (owned child)
- Certificate (owned child)
- Service (owned placeholder services)
- Endpoints (for placeholder services)

---

## Autoscaling Controllers

### 5. KPA (Knative Pod Autoscaler) Controller

**Location:** `pkg/reconciler/autoscaling/kpa/`

**Watched Resource:** `autoscaling.internal.knative.dev/v1alpha1.PodAutoscaler` (with class annotation `kpa.autoscaling.knative.dev`)

**Responsibility:**
The KPA controller implements Knative's native autoscaling algorithm, including scale-to-zero and scale-from-zero capabilities.

**Key Functions:**
- **Decider Management:** Creates/updates Deciders that make scaling decisions based on metrics
- **Metric Collection:** Reconciles Metric resources for scraping request metrics from pods
- **SKS Management:** Reconciles ServerlessService (SKS) resources to manage traffic routing during scaling
- **Pod Scaling:** Scales the underlying deployment based on Decider recommendations
- **Activation/Deactivation:**
    - Marks PA as "Activating" when scaling from zero
    - Marks PA as "Active" when sufficient pods are ready
    - Marks PA as "Inactive" when scaled to zero
- **Activator Management:** Computes how many activator pods should be in the data path based on burst capacity
- Reports actual/desired scale in status

**Informers Watched:**
- PodAutoscaler (primary, filtered by KPA class)
- ServerlessService (owned child)
- Metric (owned child)
- Pods (filtered by revision label, for counting ready pods)

---

### 6. HPA (Horizontal Pod Autoscaler) Controller

**Location:** `pkg/reconciler/autoscaling/hpa/`

**Watched Resource:** `autoscaling.internal.knative.dev/v1alpha1.PodAutoscaler` (with class annotation `hpa.autoscaling.knative.dev`)

**Responsibility:**
The HPA controller delegates autoscaling to the Kubernetes Horizontal Pod Autoscaler, useful when scale-to-zero is not needed.

**Key Functions:**
- Creates/reconciles Kubernetes **HorizontalPodAutoscaler** resources
- Configures HPA with target metrics (CPU, memory, or custom metrics)
- Reconciles **ServerlessService** in Serve mode (no activator in path)
- Propagates HPA status (current/desired replicas) to PA status
- Always marks PA as "Active" (HPA doesn't support scale-to-zero)

**Informers Watched:**
- PodAutoscaler (primary, filtered by HPA class)
- HorizontalPodAutoscaler (owned child)
- ServerlessService (owned child)
- Metric (owned child)

---

### 7. Metric Controller

**Location:** `pkg/reconciler/metric/`

**Watched Resource:** `autoscaling.internal.knative.dev/v1alpha1.Metric`

**Responsibility:**
The Metric controller manages the metrics collection infrastructure for autoscaling. It runs in the autoscaler component.

**Key Functions:**
- Registers/updates Metric resources with the metrics **Collector**
- The Collector scrapes metrics from revision pods or activator
- Updates Metric status:
    - `Ready` when collection is working
    - `NotReady` when endpoints aren't available
    - `Failed` when stats aren't being received
- Cleans up collection when Metrics are deleted

**Informers Watched:**
- Metric (primary)

---

## Networking Controllers

### 8. ServerlessService (SKS) Controller

**Location:** `pkg/reconciler/serverlessservice/`

**Watched Resource:** `networking.internal.knative.dev/v1alpha1.ServerlessService`

**Responsibility:**
The SKS controller manages the K8s Services and Endpoints that route traffic to revision pods or to the activator during scale-to/from-zero.

**Key Functions:**
- **Private Service:** Creates a K8s Service with pod selectors pointing directly to revision pods
- **Public Service:** Creates a K8s Service without selectors (endpoints managed manually)
- **Public Endpoints:** Manages the endpoints for the public service:
    - In **Serve mode:** Points to revision pod IPs (private service endpoints)
    - In **Proxy mode:** Points to activator pod IPs (for scale-to-zero)
- **Endpoint Subsetting:** When in Proxy mode, selects a consistent subset of activator endpoints per revision
- Handles transitions when pods become ready/unready
- Falls back to Proxy mode if no revision pods are ready
- Falls back to Serve mode if no activator pods are available

**Informers Watched:**
- ServerlessService (primary)
- Service (owned children)
- Endpoints (owned children + activator endpoints)

---

### 9. Certificate Controller

**Location:** `pkg/reconciler/certificate/`

**Watched Resource:** `networking.internal.knative.dev/v1alpha1.Certificate` (with class annotation `cert-manager.certificate.networking.knative.dev`)

**Responsibility:**
The Certificate controller provisions TLS certificates using cert-manager for securing external traffic.

**Key Functions:**
- Creates/reconciles **cert-manager Certificate** resources
- Configures certificates with the appropriate ClusterIssuer
- Handles **HTTP01 ACME challenges:**
    - Discovers challenge solver services created by cert-manager
    - Populates `HTTP01Challenges` in Certificate status for ingress routing
- Propagates cert-manager Certificate status to Knative Certificate
- Handles certificate renewal by detecting when renewal time has passed
- Tracks cert-manager's challenge services

**Informers Watched:**
- Knative Certificate (primary, filtered by cert-manager class)
- cert-manager Certificate (owned child)
- cert-manager Challenge (for HTTP01 challenges)
- ClusterIssuer (to determine challenge type)
- Service (tracked for challenge solver services)

---

### 10. Namespace Certificate (nscert) Controller

**Location:** `pkg/reconciler/nscert/`

**Watched Resource:** `core/v1.Namespace`

**Responsibility:**
The nscert controller automatically provisions wildcard TLS certificates for namespaces, enabling HTTPS for all services in a namespace.

**Key Functions:**
- Watches namespaces matching `namespace-wildcard-cert-selector` label selector
- Creates **wildcard Knative Certificates** for each configured domain
- Uses domain template to generate wildcard DNS names (e.g., `*.namespace.example.com`)
- Cleans up certificates when:
    - Namespace no longer matches selector
    - Domain configuration changes
- Sets appropriate certificate class annotation

**Informers Watched:**
- Namespace (primary)
- Certificate (owned children)

---

### 11. DomainMapping Controller

**Location:** `pkg/reconciler/domainmapping/`

**Watched Resource:** `serving.knative.dev/v1beta1.DomainMapping`

**Responsibility:**
The DomainMapping controller enables mapping custom domains to Knative Services or other addressable resources.

**Key Functions:**
- **Domain Claim:** Creates/validates ClusterDomainClaim to prevent hostname collisions across namespaces
- **Reference Resolution:** Resolves the target reference (typically a Knative Service) to get the backend service name
- **Ingress Creation:** Creates a Knative Ingress to route the custom domain to the target
- **TLS Management:**
    - Supports externally provided TLS secrets (`spec.tls`)
    - Auto-provisions certificates when external-domain-tls is enabled
    - Handles HTTP downgrade when certificates aren't ready
- Validates that target is in the same namespace

**Informers Watched:**
- DomainMapping (primary)
- Certificate (owned child)
- Ingress (owned child)
- ClusterDomainClaim (for domain ownership)

---

## Utility Controllers

### 12. Labeler Controller

**Location:** `pkg/reconciler/labeler/`

**Watched Resource:** `serving.knative.dev/v1.Route`

**Responsibility:**
The Labeler controller syncs routing metadata (labels/annotations) from Routes to their referenced Configurations and Revisions. This enables efficient querying and garbage collection.

**Key Functions:**
- Adds/updates labels on **Configurations** referenced by Route traffic:
    - `serving.knative.dev/route` - comma-separated list of routes targeting this config
- Adds/updates labels on **Revisions** referenced by Route traffic:
    - `serving.knative.dev/routingState` - whether revision is active (`active`), pending (`pending`), or can be GC'd (`reserve`)
- Removes routing labels when Routes are deleted (via Finalizer)
- Does NOT modify Route status (SkipStatusUpdates: true)

**Informers Watched:**
- Route (primary)
- Configuration (tracked and labeled)
- Revision (tracked and labeled)

---

### 13. Garbage Collection (GC) Controller

**Location:** `pkg/reconciler/gc/`

**Watched Resource:** `serving.knative.dev/v1.Configuration`

**Responsibility:**
The GC controller automatically deletes old, unused Revisions to prevent resource exhaustion.

**Key Functions:**
- Collects stale revisions based on configurable policies:
    - `retain-since-create-time` - minimum age before eligible for deletion
    - `retain-since-last-active-time` - minimum time since last receiving traffic
    - `min-non-active-revisions` - always keep at least this many
    - `max-non-active-revisions` - delete oldest when exceeding this count
- Never deletes:
    - `LatestReadyRevision` for a Configuration
    - Revisions with `serving.knative.dev/revision-preserved: true` annotation
    - Revisions not in `reserve` routing state
- Uses revision's `routingStateModified` timestamp to determine last active time
- Does NOT update Configuration status (SkipStatusUpdates: true)

**Informers Watched:**
- Configuration (primary)
- Revision (owned children)

---

## Controller Architecture Summary

Knative Serving controllers follow a hierarchical reconciliation pattern where higher-level resources create and manage lower-level resources. Understanding this hierarchy is key to debugging and extending the system.

### Resource Hierarchy

```
┌──────────────────────────────────────────────────────────────────────┐
│                         User-Facing Resources                        │
├──────────────────────────────────────────────────────────────────────┤
│  Service ─────► Configuration ─────► Revision                        │
│     │                                    │                           │
│     └──────────► Route                   │                           │
│                    │                     ▼                           │
│                    │           ┌─────────────────┐                   │
│                    │           │  PodAutoscaler  │                   │
│                    │           │   (KPA/HPA)     │                   │
│                    │           └────────┬────────┘                   │
│                    │                    │                            │
│                    ▼                    ▼                            │
│              ┌──────────┐      ┌─────────────────┐                   │
│              │  Ingress │      │ServerlessService│                   │
│              └──────────┘      └────────┬────────┘                   │
│                    │                    │                            │
│                    │                    ▼                            │
│                    │           ┌─────────────────┐                   │
│                    │           │  K8s Services   │                   │
│                    │           │  & Endpoints    │                   │
│                    │           └─────────────────┘                   │
│                    │                                                 │
│                    ▼                                                 │
│              ┌──────────┐                                            │
│              │Certificate│ ◄── Namespace Certificate                 │
│              └──────────┘                                            │
│                                                                      │
│  DomainMapping ──► Ingress + Certificate + ClusterDomainClaim        │
│                                                                      │
│  Labeler: Route → labels on Configuration/Revision                   │
│  GC: Configuration → deletes old Revisions                           │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Architectural Patterns

1. **Ownership & Garbage Collection:** Child resources are owned by their parent via `OwnerReferences`. When a parent is deleted, Kubernetes automatically garbage collects all owned children.

2. **Status Propagation:** Status flows upward through the hierarchy. For example:
    - Deployment readiness → Revision status
    - Revision readiness → Configuration status (`LatestReadyRevisionName`)
    - Configuration + Route status → Service status

3. **Reconciliation Triggers:** Controllers are triggered by:
    - Changes to their primary watched resource
    - Changes to owned child resources
    - Changes to tracked (but not owned) resources

4. **Idempotency:** All reconcilers are designed to be idempotent. Running reconciliation multiple times with the same input produces the same result.

5. **Controller Separation:** Controllers run in two binaries:
    - **controller** (`cmd/controller/`): Runs most controllers (Service, Configuration, Revision, Route, SKS, Certificate, etc.)
    - **autoscaler** (`cmd/autoscaler/`): Runs Metric controller and the autoscaling decision logic

### Data Flow Summary

#### 1. Deployment Flow (Creating a new Service)

When you create a Knative Service, a cascade of resource creation occurs. The **Service controller** creates a Configuration (which defines the desired state of your app) and a Route (which defines how traffic reaches it). The **Configuration controller** then creates a Revision, an immutable snapshot of your code and config. Finally, the **Revision controller** creates the actual Kubernetes Deployment (to run your pods), a PodAutoscaler (to manage scaling), and optionally an Image cache resource (to pre-pull images to nodes).

```
User creates Service
        │
        ▼
┌───────────────────┐     ┌─────────────────┐     ┌────────────┐
│ Service Controller│────►│  Configuration  │────►│  Revision  │
└───────────────────┘     │    Controller   │     │ Controller │
                          └─────────────────┘     └─────┬──────┘
                                                        │
                          ┌─────────────────────────────┼─────────────────────────────┐
                          │                             │                             │
                          ▼                             ▼                             ▼
                   ┌────────────┐              ┌───────────────┐              ┌─────────────┐
                   │ Deployment │              │ PodAutoscaler │              │ Image Cache │
                   │  (K8s)     │              │   (PA)        │              │ (optional)  │
                   └────────────┘              └───────────────┘              └─────────────┘
```

#### 2. Traffic Routing Flow (Serving a request)

The **Route controller** creates a Knative Ingress resource that describes how external traffic should reach your Revisions. An ingress controller (Istio, Kourier, or Contour, depending on your installation) watches these Ingress resources and configures the actual load balancer/proxy. Traffic ultimately flows to Kubernetes Services managed by the ServerlessService (SKS) controller, which point to your revision pods.

```
┌─────────────────┐     ┌─────────────┐     ┌─────────────────────────────┐
│ Service/Route   │────►│   Ingress   │────►│  Ingress Controller         │
│   Controller    │     │  (Knative)  │     │  (Istio/Kourier/Contour)    │
└─────────────────┘     └─────────────┘     └──────────────┬──────────────┘
                                                           │
                                                           ▼
                                            External traffic routed to
                                            K8s Services created by SKS
```

#### 3. Autoscaling Flow (Normal operation with traffic)

During normal operation when pods are running, the **KPA controller** keeps the SKS in "Serve" mode, meaning the public Kubernetes Service endpoints point directly to your revision pod IPs. Meanwhile, the **Metric controller** (running in the autoscaler component) manages a Collector that continuously scrapes request metrics from your pods. These metrics feed into the autoscaler's Decider, which determines the desired replica count.

```
┌──────────────────┐     ┌─────────────┐     ┌────────────────────┐
│  KPA Controller  │────►│     SKS     │────►│ Public K8s Service │
│                  │     │  (Serve)    │     │ Points to Pod IPs  │
└────────┬─────────┘     └─────────────┘     └────────────────────┘
         │
         ▼
┌──────────────────┐     ┌─────────────┐     ┌────────────────────┐
│ Metric Controller│────►│  Collector  │────►│ Scrapes metrics    │
│  (in autoscaler) │     │             │     │ from revision pods │
└──────────────────┘     └─────────────┘     └────────────────────┘
```

#### 4. Scale-to-Zero Flow

When no requests arrive for the configured idle period (default: 30s), the **Autoscaler** (via its Decider) determines the desired scale is zero. The **KPA controller** reads this decision and scales the Deployment to zero replicas. Before doing so, it switches the SKS to "Proxy" mode, which changes the public Service endpoints to point to the **Activator** instead of the (now non-existent) pods. The Activator is a shared component that can buffer requests and trigger scale-up. The PodAutoscaler status is marked as "Inactive".

```
No requests for idle period
         │
         ▼
┌──────────────────┐
│    Autoscaler    │
│ Decider: scale=0 │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  KPA Controller  │
│ applies decision │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     ┌─────────────────────┐
│  SKS switches    │────►│ Public K8s Service  │
│  to Proxy mode   │     │ Points to ACTIVATOR │
└──────────────────┘     └─────────────────────┘
         │
         ▼
   PA Status: Inactive
   Revision has 0 pods
```

#### 5. Scale-from-Zero Flow (Cold Start)

When a request arrives for a scaled-to-zero revision, it hits the Activator (since SKS is in Proxy mode). The **Activator** buffers the request and reports metrics/stats to the autoscaler. The **KPA controller** sees this demand and scales the Deployment up to at least `minScale` (or 1 if not set). Once pods become ready, the KPA switches SKS back to "Serve" mode, pointing traffic directly to pods. The Activator then proxies any buffered requests to the now-ready pods and removes itself from the data path.

```
Request arrives at Activator
         │
         ▼
┌──────────────────┐
│    Activator     │
│  buffers request │
└────────┬─────────┘
         │ reports stats
         ▼
┌──────────────────┐     ┌──────────────────┐
│  KPA Controller  │────►│ Scales Deployment│
│  sees demand     │     │ to minScale (≥1) │
└────────┬─────────┘     └──────────────────┘
         │
         ▼ (when pods ready)
┌──────────────────┐     ┌─────────────────────┐
│  SKS switches    │────►│ Public K8s Service  │
│  to Serve mode   │     │ Points to POD IPs   │
└──────────────────┘     └─────────────────────┘
         │
         ▼
   Activator proxies buffered
   request, then exits data path
```

#### 6. TLS Certificate Flow

When external-domain-tls is enabled, the **Route controller** (or **DomainMapping controller**) creates a Knative Certificate resource for the domain. The **Certificate controller** then creates a cert-manager Certificate, which triggers cert-manager to obtain a certificate from a CA (typically Let's Encrypt via ACME). Once issued, cert-manager stores the certificate in a Kubernetes Secret. The Ingress controller uses this Secret to terminate TLS for your domain.

```
┌─────────────────────┐     ┌─────────────────┐     ┌──────────────────┐
│ Route Controller    │────►│ Knative         │────►│ Certificate      │
│ or DomainMapping    │     │ Certificate     │     │ Controller       │
└─────────────────────┘     └─────────────────┘     └────────┬─────────┘
                                                             │
                                                             ▼
                                                    ┌──────────────────┐
                                                    │ cert-manager     │
                                                    │ Certificate      │
                                                    └────────┬─────────┘
                                                             │ (ACME/CA)
                                                             ▼
                                                    ┌──────────────────┐
                                                    │ K8s TLS Secret   │
                                                    │ (used by Ingress)│
                                                    └──────────────────┘
```

---

## Configuration

Controllers are configured via ConfigMaps in the `knative-serving` namespace:

| ConfigMap | Controllers Using It |
|-----------|---------------------|
| `config-autoscaler` | KPA, HPA, Metric |
| `config-defaults` | Revision, Service |
| `config-deployment` | Revision, KPA, HPA |
| `config-domain` | Route, Namespace Certificate |
| `config-gc` | GC |
| `config-network` | Route, DomainMapping, Certificate, Namespace Certificate |
| `config-observability` | Revision |
| `config-certmanager` | Certificate |

---

## Entry Point

All controllers are instantiated in `cmd/controller/main.go` which:
1. Sets up shared informers
2. Instantiates each controller via their `NewController` functions
3. Starts the shared informer factory
4. Runs all controllers with the configured concurrency
