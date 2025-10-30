# Cerebrium KServing Fork - AI Assistant Guide

## Repository Context

This is a fork of [Knative Serving](https://github.com/knative/serving) based on the **release-1.18 branch**. This fork contains custom modifications to enhance Knative Serving for Cerebrium's serverless infrastructure needs.

### Fork Details

- **Upstream**: `knative/serving` (release-1.18)
- **Organization**: CerebriumAI
- **Primary Branch**: `feat/proxy-queue-time-threshold-metrics`
- **Main Branch for PRs**: `main`

## Key Modifications from Upstream

### 1. Enhanced Autoscaling & Load Management

- **Scale Buffer Support**: Added configurable scale buffer to handle traffic spikes
- **Pod Health Tracking & Quarantine System**: Implements sophisticated health monitoring with automatic quarantine for failing pods
- **Effective Capacity Tracking**: Enhanced capacity calculations that account for pod health states
- **Consistent Hashing**: Added consistent hashing for endpoint distribution to activators

### 2. Activator & Throttler Improvements

- **Enhanced Throttling**: Improved activator throttling and load balancing mechanisms
- **Error Propagation**: Better error handling between proxy and throttler components
- **Request Queue Monitoring**: Added breaker pending requests metric
- **502 Error Handling**: Detailed 502 error messages and healthy target gauge metrics

### 3. Queue Proxy Enhancements

- **WebSocket Support**: Added WebSocket protocol support to queue proxy
- **Aggressive Health Probes**: Made queue-proxy health probes more aggressive for faster failure detection
- **Request ID Tracking**: Uses run-id when available instead of x-request-id for better tracing
- **Proxy Latency Metrics**: Added comprehensive latency tracking

### 4. Metrics & Observability

- **Quarantine Metrics**: Counter metrics for pod quarantine events
- **Queue Time Threshold Metrics**: Tracking queue time for performance monitoring
- **Proxy Start Logging**: Added logging when proxy starts for better debugging
- **Health Tracking Metrics**: Comprehensive health state tracking for all pods

## Important Files & Components

### Core Modified Components

**Activator** (`pkg/activator/net/`): Load balancing, health tracking, quarantine system
- `pod_tracker.go`: Pod state machine and lifecycle management
- `quarantine.go`: Health check and quarantine system
- `state_manager.go`: State update queue, worker, and supervisor
- `revision_throttler.go`: Per-revision routing and capacity management
- `throttler.go`: Global multi-revision orchestration

**Queue Proxy** (`pkg/queue/`): WebSocket support, enhanced metrics, pod registration client

**Autoscaler** (`pkg/autoscaler/`): Scale buffer, effective capacity calculations

**Revision Lifecycle** (`pkg/activator/net/revision_lifecycle.go`): uint64 containerConcurrency API

### Documentation

- **ACTIVATOR_FIXES.md**: Detailed documentation of known issues and planned fixes for the activator health tracking system

## Development Guidelines

### Building & Testing

```bash
# Run tests
go test ./...

# Run specific package tests
go test ./pkg/activator/...

# Build the project
./hack/build.sh

# Update code generation
./hack/update-codegen.sh
```

### Code Style & Conventions

- Follow existing Go conventions in the Knative project
- Use atomic operations for concurrent access to shared state
- Implement proper error propagation through all layers
- Add comprehensive metrics for new features
- Include unit tests for all modifications

### Common Commands

```bash
# Check git status
git status

# View recent changes
git log --oneline -10

# Compare with upstream
git diff upstream/release-1.18..HEAD

# Run linters (if available)
# Check for: golangci-lint, gofmt, etc.
```

## Key Technical Decisions

### Pod State Management (Feature Gate-Based Operation)

The activator implements a simplified 4-state pod state machine with two independent feature gates:
- **`enableQPAuthority`** (default: `true`): Controls whether queue-proxy events trigger state changes
- **`enableQuarantine`** (default: `true`): Controls health-check and quarantine system

**Pod States** (simplified from 6 to 4 states):
- `podReady (0)`: Pod is ready to serve traffic
- `podQuarantined (1)`: Pod failed health check (only used when `enableQuarantine=true`)
- `podRecovering (2)`: Pod recovering from quarantine (only used when `enableQuarantine=true`)
- `podNotReady (3)`: Pod not ready to receive traffic (NOT routable)

**Removed States** (simplified in commit cea5c3e487):
- `podDraining`: Merged into `podNotReady` (draining pods transition to not-ready)
- `podRemoved`: Pods are simply deleted from the tracker map (no terminal state needed)

#### Operating Modes

**Mode 1: Hybrid (QP Authority ON, Quarantine ON)** - Production default (BOTH ENABLED)
- Both systems active simultaneously
- QP events control base state, health checks can quarantine
- Most comprehensive protection mode
- Pods start in `podNotReady`, promoted by QP "ready" event
- Health checks run and can quarantine failing pods
- States used: All 4 states

**Mode 2: QP Authority ON, Quarantine OFF**
- Queue-proxy events are authoritative for state changes
- No health checks or quarantine logic
- Pods start in `podNotReady`, promoted by QP "ready" event
- States used: `podReady`, `podNotReady`

**Mode 3: QP Authority OFF, Quarantine ON**
- Queue-proxy sends events but activator logs and ignores them
- K8s informer is sole authority for pod state
- Pods start in `podReady` state immediately
- Health checks run and can quarantine failing pods
- States used: `podReady`, `podQuarantined`, `podRecovering`, `podNotReady`

**Mode 4: Minimal (Both OFF)**
- K8s informer only, no health checks, no QP authority
- Simplest mode, least protection
- States used: `podReady`, `podNotReady`

#### ConfigMap Configuration

Feature gates are configured via the `config-features` ConfigMap in the `knative-serving` namespace:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-features
  namespace: knative-serving
data:
  # Queue-proxy pod authority (default: enabled)
  # Controls whether queue-proxy push events are authoritative for pod state
  # When enabled: Sub-second cold starts via push-based registration
  # When disabled: Falls back to K8s informer (60-70s cold start delay)
  queueproxy.pod-authority: "enabled"

  # Activator pod quarantine (default: enabled)
  # Controls whether activator performs health checks and quarantines failing pods
  # When enabled: Pods failing health checks are temporarily quarantined
  # When disabled: No health checks, relies solely on K8s readiness probes
  activator.pod-quarantine: "enabled"
```

**Configuration options:**
- `"enabled"`: Feature is active
- `"disabled"`: Feature is inactive

**Dynamic updates:** Changes to the ConfigMap are applied at runtime without requiring pod restarts. The activator watches the ConfigMap and updates feature gates dynamically.

**Use cases:**
- **Emergency rollback**: Disable `queueproxy.pod-authority` if push-based registration causes issues
- **Gradual rollout**: Enable features progressively across environments
- **Debugging**: Disable features to isolate issues
- **Performance tuning**: Test different operating modes under load

#### State Transition Map (Hybrid Mode - All Features Enabled)

```
NEW POD → podNotReady ⇄ podReady ⇄ podQuarantined
                                    ⇅
                                podRecovering
```

**From podNotReady (new pods waiting for ready signal):**
- ✅ QP "ready" event → `podReady` (requires `enableQPAuthority=true`)
- ✅ K8s informer "healthy" → `podReady` (if QP hasn't said "not-ready" recently, or `enableQPAuthority=false`)
- 🗑️ K8s informer removed → deleted from map (if refCount == 0)
- 🗑️ QP "draining" event → stays `podNotReady` (crash before ready, requires `enableQPAuthority=true`)

**From podReady (established ready pods):**
- ⚠️ QP "not-ready" event → `podNotReady` (preserves refCount/breaker, requires `enableQPAuthority=true`)
- ❌ Health check failure → `podQuarantined` (requires `enableQuarantine=true`)
- 🗑️ QP "draining" event → `podNotReady` (graceful shutdown, requires `enableQPAuthority=true`)
- 🗑️ K8s informer removed → `podNotReady`, then deleted from map (if refCount == 0)

**From podQuarantined (health check failures):**
- ⏱️ After backoff → `podRecovering` (requires `enableQuarantine=true`)
- 🗑️ K8s informer removed → deleted from map

**From podRecovering (recovering from quarantine):**
- ✅ Health check success → `podReady` (requires `enableQuarantine=true`)
- ❌ Health check failure → `podQuarantined` (requires `enableQuarantine=true`)
- 🗑️ K8s informer removed → `podNotReady`, then deleted from map

**Pod Removal:**
- Pods are deleted from the tracker map (not transitioned to a removed state)
- Deletion happens immediately when refCount == 0 and K8s removes the pod
- Active requests prevent deletion until they complete (refCount > 0)

#### Queue-Proxy Authority Model (when `enableQPAuthority=true`)

**Trust Hierarchy:**
1. **Queue-Proxy Push (AUTHORITATIVE when enabled)** - Pod knows its own state best
   - QP data < 30s old: QP state overrides K8s informer
   - QP events: "startup", "ready", "not-ready", "draining"

2. **K8s Informer (FALLBACK)** - Trusted when QP silent or gate disabled
   - QP data > 60s old: Trust informer (QP likely dead/crashed)
   - QP never heard from: Informer is sole authority
   - `enableQPAuthority=false`: Informer is always authoritative

**QP Events (require `enableQPAuthority=true` to trigger state changes):**
- `startup`: Creates podNotReady tracker (not viable for traffic)
- `ready`: Promotes podNotReady → podReady (now viable)
- `not-ready`: Demotes podReady → podNotReady (stops new traffic, preserves active requests)
- `draining`: Transitions podReady → podNotReady (graceful shutdown, preserves refCount)

**Informer Override Rules (when `enableQPAuthority=true`):**
- ✅ QP "not-ready" < 30s ago → Informer "healthy" IGNORED
- ✅ QP "ready" < 30s ago → Informer "draining" IGNORED
- ✅ QP silent > 60s → Informer is AUTHORITATIVE (QP likely dead)
- ✅ No QP data → Informer is AUTHORITATIVE

**Critical Invariants:**
- `podNotReady` pods NEVER receive traffic (excluded from routing)
- State transitions preserve breaker capacity and refCount
- Active requests complete even when pod demoted to not-ready
- Only atomic CAS operations for state transitions
- Health checks are independent (controlled by `enableQuarantine`)
- Quarantine/recovery states can coexist with QP authority states

### Capacity Calculation

- Capacity based on ALL routable trackers (podReady + podRecovering when `enableQuarantine=true`)
- Filtering at routing time excludes non-routable pods (podNotReady, podQuarantined)
- Prevents capacity starvation while pods are starting up
- Dynamic adjustment based on QP readiness signals (when `enableQPAuthority=true`)
- Quarantine system can reduce capacity when pods fail health checks (when `enableQuarantine=true`)

## Implementation Details

### Code Organization (Modular Architecture)

**Major refactoring in commit cea5c3e487** - Reorganized activator code into focused modules with clear separation of concerns:

**File Structure** (`pkg/activator/net/`, ~3,178 lines total):

1. **`pod_tracker.go` (598 lines)**: Pod data structure + state machine logic
   - Pod state definitions and validation
   - State transition helpers and decision logic
   - Reference counting and capacity management
   - QP freshness tracking

2. **`quarantine.go` (196 lines)**: Health check & quarantine system
   - Health check infrastructure (TCP ping, HTTP readiness)
   - Quarantine/recovery logic
   - Exponential backoff for recovery attempts

3. **`state_manager.go` (605 lines)**: State update queue & worker infrastructure
   - Serialized state update processing via work queue
   - Operation handlers (mutatePod, recalculateAll, updateCapacity, cleanup)
   - Supervisor pattern for worker lifecycle management
   - Panic recovery with exponential backoff

4. **`revision_throttler.go` (1,217 lines)**: Per-revision request routing & capacity
   - Load balancing policy selection (random, round-robin, first-available)
   - Request routing and tracker assignment
   - Capacity calculations and breaker integration
   - Pod registration handlers

5. **`throttler.go` (607 lines, reduced from ~1,800)**: Global Throttler orchestration
   - Multi-revision coordination
   - Feature gate management
   - Metrics collection
   - Endpoint watching

**Benefits:**
- Clear separation of concerns - each file has single responsibility
- State machine rules encapsulated in pod_tracker.go
- Better maintainability and testability
- Reduced lock contention through modular design
- Easier to understand control flow

### State Machine Documentation

Comprehensive state machine documentation is available as inline comments in `pkg/activator/net/throttler.go` (lines 182-400). The documentation includes:

- ASCII art state diagram showing all transitions
- Detailed descriptions of each of the 4 pod states
- Entry and exit conditions for every state
- Routable vs non-routable state classifications
- Queue-proxy authority model and trust hierarchy
- Informer override rules with time windows
- Critical invariants that must be maintained
- Capacity calculation rules

This documentation serves as the authoritative reference for understanding pod lifecycle management in the activator.

### Worker Lifecycle Management

**Supervisor Pattern** (commit cea5c3e487): The state worker is managed by a supervisor goroutine that handles panics and restarts:

- **Worker**: Processes state updates from the queue, panics are recovered
- **Supervisor**: Detects worker exit and restarts with clean channels
- **Exponential Backoff** (commit 847383defd): Prevents tight panic loops
  - Initial backoff: 100ms
  - Max backoff: 5s
  - Reset window: 1 minute (successful operation resets backoff)
  - Tracks last panic time for intelligent restart decisions

**Benefits:**
- Prevents panic death loops
- No double-close issues on channels
- Graceful degradation under persistent bugs
- Clean shutdown coordination via `supervisorDone` channel

### State Update Queue & Metrics

**Work Queue Pattern**: All state mutations serialized through `stateUpdateChan` to prevent race conditions.

**Queue Metrics** (commit cea5c3e487):
- `stateUpdateQueueTime`: Time requests spend waiting in queue (reveals saturation)
- `stateUpdateProcessingTime`: Time to process each request (reveals slow operations)
- No dropped updates - queue blocks until processed (reliability over throughput)

**Operations** (refactored in commit e7d0573074):
- `opMutatePod`: Handle QP events (ready, not-ready, draining) - renamed from opAddPod
- `opRemovePod`: Immediate removal of stale trackers (IP reuse detection) - reintroduced for IP reuse scenarios
- `opRecalculateAll`: Full reconciliation from K8s endpoints
- `opUpdateCapacity`: Recalculate capacity after activator count changes
- `opCleanupStalePods`: Periodic cleanup of stale not-ready pods (10 min threshold)

### State Reason Tracking (Observability)

**Added in commit e7d0573074**: Each state transition tracks the reason for the change:

- `qp-ready`: QP promoted pod to ready
- `qp-not-ready`: QP demoted pod to not-ready (readiness probe failed)
- `qp-draining`: QP initiated graceful shutdown
- `informer-added`: K8s informer created new pod
- `informer-ready`: K8s informer promoted pod to ready
- `informer-promoted`: K8s promoted not-ready to ready (stale QP data)
- `informer-removed`: K8s informer removed pod

**Benefits:**
- Clear audit trail of state transitions
- Easier debugging of pod lifecycle issues
- Distinguishes between QP-driven vs informer-driven transitions

### IP Reuse Detection & Stale Tracker Removal

**Problem**: In Cerebrium's infrastructure, pod IPs can be reused across different revisions. This creates stale trackers where a revisionThrottler holds a reference to a podTracker with an IP that now belongs to a different revision.

**Solution**: Multi-layer IP reuse detection with immediate removal via `opRemovePod`:

**Detection Points:**

1. **RevisionManager Probes** (`revision_backends.go:164-211`)
   - Earliest detection point - validates revision headers during initial pod probing
   - Prevents stale IPs from ever being added to `healthyPods` set
   - Checks `X-Knative-Revision-Name` and `X-Knative-Revision-Namespace` headers
   - Probe fails if headers don't match expected revision
   - **Benefit**: Most efficient - stale IPs never reach the throttler

2. **Quarantine Health Checks** (`quarantine.go:143-207`)
   - Validates revision headers during health check probes
   - Returns `isStaleTracker=true` when IP reuse detected
   - Caller enqueues `opRemovePod` via background goroutine (fire-and-forget)
   - **Benefit**: Catches IP reuse during ongoing health monitoring

3. **Routing Validation** (`revision_throttler.go:598-621`)
   - Validates `tracker.revisionID != rt.revID` during tracker acquisition
   - Enqueues `opRemovePod` via background goroutine when mismatch detected
   - **Benefit**: Last-line defense - catches any stale trackers that slipped through

**`opRemovePod` Operation** (`state_manager.go:373-411`):
- **Strict removal**: Deletes tracker immediately regardless of state or refCount
- **Force removal**: No graceful draining - requests routed to wrong revision are discarded
- **Metrics cleanup**: Properly decrements quarantine gauges if needed
- **Idempotent**: Safe to call multiple times on same pod
- **Capacity update**: Recalculates capacity after removal

**Benefits:**
- Prevents routing requests to wrong revisions
- Automatic cleanup without operator intervention
- Multi-layer defense (probe, health check, routing)
- No performance impact on hot path (fire-and-forget goroutines)

### Performance Optimizations & Type Safety

**Work Queue Serialization:**
The work queue pattern (commit cea5c3e487) **eliminated race windows** in pod tracker creation:

- All state mutations serialized through `stateUpdateChan` worker
- `processMutatePod` holds write lock for entire tracker creation and insertion
- No TOCTOU races - lock held from existence check through map insertion
- **Trade-off**: Slightly increased lock hold time vs. zero race windows
- Previous implementation had intentional race window with double-check - no longer needed

**uint64 Migration** (commit 6f3f7cad9c): Complete type migration to eliminate integer overflow risks:
- `GetContainerConcurrency()`: Changed return type from int64 → uint64
- Validates at API boundary where K8s data enters the system
- All capacity calculations now use uint64 throughout the pipeline
- Eliminates need for unsafe conversions and prevents overflow in high-concurrency scenarios

**Time Unit Consistency** (commit e7d0573074): Standardized on microsecond precision:
- All timestamp fields use `UnixMicro()` consistently
- Fixed incorrect age calculations due to mixed `Unix()`/`UnixMicro()` usage
- `PodNotReadyStaleThreshold` extracted as package constant (10 minutes)
- Better precision for time-sensitive operations (QP freshness, cleanup, backoff)

## Known Issues & TODOs

See `ACTIVATOR_FIXES.md` for detailed list of known issues.

**Recent Changes** (last 30 commits):

**Architecture & State Machine:**
- ✅ Simplified state machine from 6 → 4 states (commit cea5c3e487)
- ✅ Modular code organization into 5 focused files (commit cea5c3e487)
- ✅ Supervisor pattern for worker lifecycle management (commit cea5c3e487)
- ✅ Exponential backoff for worker restarts (commit 847383defd)
- ✅ State reason tracking for observability (commit e7d0573074)

**Operations & Cleanup:**
- ✅ Renamed `opAddPod` → `opMutatePod` (commit e7d0573074)
- ✅ Reintroduced `opRemovePod` for stale tracker detection (IP reuse scenarios)
- ✅ Fixed aggressive pod removal in mutate operations (commit 847383defd)
- ✅ Informer-driven immediate removal for zero refCount pods (commit 847383defd)
- ✅ Periodic cleanup for stale not-ready pods (10 min threshold, commit e7d0573074)
- ✅ IP reuse detection in health checks and routing paths
- ✅ Early IP reuse detection in revisionManager probes (prevents stale IPs from reaching throttler)

**Metrics & Observability:**
- ✅ `stateUpdateQueueTime` metric - reveals queue saturation (commit cea5c3e487)
- ✅ `stateUpdateProcessingTime` metric - reveals slow operations (commit cea5c3e487)
- ✅ State reason tracking (qp-ready, qp-draining, informer-removed, etc., commit e7d0573074)
- ✅ Removed state update timeouts - no dropped updates (commit cea5c3e487)

**Type Safety & Consistency:**
- ✅ uint64 migration for containerConcurrency (commit 6f3f7cad9c)
- ✅ Time unit consistency - standardized on UnixMicro (commit e7d0573074)
- ✅ Magic numbers extracted to package constants (commit 31c23428b5, e7d0573074)

**Bug Fixes & Correctness:**
- ✅ Fixed nil pointer bug - missing logger initialization (commit e7d0573074)
- ✅ Self-transitions allowed in state validation (commit 31c23428b5)
- ✅ Optimized lock usage - consolidated RLock acquisitions (commit 31c23428b5)
- ✅ Refactored decreaseWeight closure to method receiver (commit 31c23428b5)

**Testing & Quality:**
- ✅ Removed all time.Sleep() calls from tests (commit 6af4298306)
- ✅ Removed blocking test helpers (updateThrottlerState, commit 6af4298306)
- ✅ Comprehensive test coverage for all operating modes (commit 31c23428b5)
- ✅ All tests pass with race detection enabled (ongoing)

## Testing Considerations

When making changes:

1. **Race Detection**: Always run tests with `-race` flag: `go test -race ./pkg/activator/net`
2. **Operating Modes**: Test all feature gate combinations (QP authority ON/OFF, Quarantine ON/OFF)
3. **QP Event Sequences**: Test (startup, ready, not-ready, draining) with `enableQPAuthority=true`
4. **Quarantine Cycles**: Test quarantine → recovering → ready with `enableQuarantine=true`
5. **Hybrid Mode**: Verify both systems work together correctly
6. **QP Authority**: Verify QP overrides K8s informer when fresh (< 30s)
7. **Trust Transitions**: Test QP staleness (60s) triggers informer authority
8. **State Invariants**: Verify state transitions preserve breaker capacity and refCount
9. **Routing Exclusion**: Ensure podNotReady and podQuarantined excluded from routing
10. **Graceful Shutdown**: Test not-ready transitions preserve active requests (refCount > 0)
11. **State Reasons**: Verify state reason tracking for all transitions
12. **Queue Saturation**: Monitor stateUpdateQueueTime metric under load
13. **Panic Recovery**: Verify supervisor restarts worker with exponential backoff
14. **Time Precision**: Verify UnixMicro() used consistently for timestamps
15. **WebSocket Compatibility**: Check if modifying proxy code
16. **IP Reuse Detection**: Test stale tracker detection via revision header validation
17. **opRemovePod**: Verify immediate removal regardless of state or refCount

**Key Test Files:**
- `pkg/activator/net/throttler_default_test.go` - Default mode (QP authority OFF, Quarantine ON)
- `pkg/activator/net/qp_authority_test.go` - QP authority mode tests and event sequences
- `pkg/activator/net/throttler_hybrid_test.go` - Hybrid mode (both features enabled)
- `pkg/activator/net/throttler_test.go` - Core throttler functionality and breaker tests
- `pkg/activator/net/throttler_queue_test.go` - Work queue, panic recovery, and supervisor tests
- `pkg/activator/net/throttler_race_test.go` - Concurrent operation and race condition tests
- `pkg/activator/net/state_machine_test.go` - State transition validation and self-transition tests
- `pkg/activator/net/stale_tracker_test.go` - IP reuse detection and opRemovePod tests
- `pkg/activator/net/pod_registration_handler_test.go` - Handler validation
- `pkg/queue/pod_registration_test.go` - Client-side registration

## Deployment Notes

This fork is designed for Cerebrium's infrastructure and includes:

- ECR repository integration for container images
- Custom scaling parameters optimized for ML workloads
- Enhanced monitoring for serverless function health

## Contributing

When contributing to this fork:

1. Branch from the appropriate feature branch
2. Follow the existing code patterns and conventions
3. Add tests for new functionality
4. Update metrics and logging appropriately
5. Document significant changes in commit messages

## Contact & Support

For questions about this fork's modifications, refer to:

- The git history for detailed change context
- ACTIVATOR_FIXES.md for known issues
- Upstream Knative documentation for base functionality
