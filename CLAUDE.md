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

- **Activator** (`pkg/activator/`): Load balancing, health tracking, quarantine system
- **Queue Proxy** (`pkg/queue/`): WebSocket support, enhanced metrics
- **Autoscaler** (`pkg/autoscaler/`): Scale buffer, effective capacity calculations
- **Throttler** (`pkg/activator/throttler.go`): Enhanced error handling, pod state management

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

The activator implements a flexible 6-state pod state machine with two independent feature gates:
- **`enableQPAuthority`** (default: `true`): Controls whether queue-proxy events trigger state changes
- **`enableQuarantine`** (default: `true`): Controls health-check and quarantine system

**Pod States:**
- `podReady (0)`: Pod is ready to serve traffic
- `podDraining (1)`: Pod is being removed from service, draining active requests (requires `enableQPAuthority=true`)
- `podQuarantined (2)`: Pod failed health check (only used when `enableQuarantine=true`)
- `podRecovering (3)`: Pod recovering from quarantine (only used when `enableQuarantine=true`)
- `podRemoved (4)`: Pod completely removed from tracker (terminal state)
- `podNotReady (5)`: Pod not ready to receive traffic (NOT routable)

#### Operating Modes

**Mode 1: Hybrid (QP Authority ON, Quarantine ON)** - Production default (BOTH ENABLED)
- Both systems active simultaneously
- QP events control base state, health checks can quarantine
- Most comprehensive protection mode
- Pods start in `podNotReady`, promoted by QP "ready" event
- Health checks run and can quarantine failing pods
- States used: All 6 states

**Mode 2: QP Authority ON, Quarantine OFF**
- Queue-proxy events are authoritative for state changes
- No health checks or quarantine logic
- Pods start in `podNotReady`, promoted by QP "ready" event
- States used: `podReady`, `podDraining`, `podNotReady`, `podRemoved`

**Mode 3: QP Authority OFF, Quarantine ON**
- Queue-proxy sends events but activator logs and ignores them
- K8s informer is sole authority for pod state
- Pods start in `podReady` state immediately
- Health checks run and can quarantine failing pods
- States used: `podReady`, `podQuarantined`, `podRecovering`, `podNotReady`, `podRemoved`

**Mode 4: Minimal (Both OFF)**
- K8s informer only, no health checks, no QP authority
- Simplest mode, least protection
- States used: `podReady`, `podNotReady`, `podRemoved`

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
NEW POD → podNotReady → podReady ←→ podQuarantined
            ↓              ↓              ↓
            └──────────→ podDraining ←────┘
                           ↓
                        podRemoved
                           ↓
                  (Re-add: podRecovering)
```

**From podNotReady (new pods waiting for ready signal):**
- ✅ QP "ready" event → `podReady` (requires `enableQPAuthority=true`)
- ✅ K8s informer "healthy" → `podReady` (if QP hasn't said "not-ready" recently, or `enableQPAuthority=false`)
- 🗑️ K8s informer removed → `podRemoved` (cleanup)
- 🗑️ QP "draining" event → `podDraining` (crash before ready, requires `enableQPAuthority=true`)

**From podReady (established ready pods):**
- ⚠️ QP "not-ready" event → `podNotReady` (preserves refCount/breaker, requires `enableQPAuthority=true`)
- ❌ Health check failure → `podQuarantined` (requires `enableQuarantine=true`)
- 🗑️ QP "draining" event → `podDraining` (requires `enableQPAuthority=true`)
- 🗑️ K8s informer removed → `podDraining` or `podNotReady` (depends on feature gates)

**From podQuarantined (health check failures):**
- ⏱️ After backoff → `podRecovering` (requires `enableQuarantine=true`)
- 🗑️ K8s informer removed → `podRemoved`

**From podRecovering (recovering from quarantine):**
- ✅ Health check success → `podReady` (requires `enableQuarantine=true`)
- ❌ Health check failure → `podQuarantined` (requires `enableQuarantine=true`)
- 🗑️ K8s informer removed → `podDraining`

**From podDraining (graceful shutdown):**
- ⏱️ RefCount reaches 0 → `podRemoved` (all requests complete)
- ⏱️ Stuck for maxDrainingDuration → `podRemoved` (force remove)
- 🔄 Re-added to endpoints → `podNotReady` (treat as new pod)

**From podRemoved (terminal state):**
- 🔄 Re-added to endpoints → `podNotReady` (treat as new pod)

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
- `draining`: Transitions podReady → podDraining (graceful shutdown)

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
- Filtering at routing time excludes non-routable pods (podNotReady, podQuarantined, podDraining, podRemoved)
- Prevents capacity starvation while pods are starting up
- Dynamic adjustment based on QP readiness signals (when `enableQPAuthority=true`)
- Quarantine system can reduce capacity when pods fail health checks (when `enableQuarantine=true`)

## Implementation Details

### State Machine Documentation

Comprehensive state machine documentation is available as inline comments in `pkg/activator/net/throttler.go` (lines 182-400). The documentation includes:

- ASCII art state diagram showing all transitions
- Detailed descriptions of each of the 6 pod states
- Entry and exit conditions for every state
- Routable vs non-routable state classifications
- Queue-proxy authority model and trust hierarchy
- Informer override rules with time windows
- Critical invariants that must be maintained
- Capacity calculation rules

This documentation serves as the authoritative reference for understanding pod lifecycle management in the activator.

### Performance Optimizations

**Race Window in addPodIncremental:**
The `addPodIncremental` function (throttler.go:2073-2117) contains an intentional race window where the lock is released during tracker creation. This is a deliberate performance trade-off:

- **Trade-off**: Minimizes lock hold time at the cost of potential wasted allocations
- **Correctness**: Race is detected and handled safely via double-check after re-acquiring lock
- **Impact**: Rare (<1% expected) and only causes allocation waste, never incorrect state
- **Alternative**: Considered sync.Once pattern but current approach prioritizes throughput

See inline documentation at throttler.go:2075-2117 for detailed analysis.

## Known Issues & TODOs

See `ACTIVATOR_FIXES.md` for detailed list of known issues.

**Recent Changes:**
- ✅ ConfigMap-based feature gate configuration (commit 21cc1f1)
- ✅ Comprehensive state machine inline documentation (commit d37fd14)
- ✅ Simplified probe state machine to 2 states (commit b7f6294)
- ✅ Race window documentation with trade-off analysis (commit 998c021)
- ✅ Dual feature gates implemented for backward compatibility (commit 2d290ffca)
- ✅ Quarantine system re-added behind `enableQuarantine` gate (commit 2d290ffca)
- ✅ QP authority behind `enableQPAuthority` gate (commit 2d290ffca)
- ✅ Multi-mode test coverage for all operating modes (commit bdd99b969)
- ✅ State machine validation and comprehensive metrics (commits 64ab3090b, bbf7c5279)

## Testing Considerations

When making changes:

1. Run unit tests with race detection: `go test -race ./pkg/activator/net`
2. Test all operating modes using feature gate combinations
3. Test QP event sequences (startup, ready, not-ready, draining) with `enableQPAuthority=true`
4. Test quarantine/recovery cycles with `enableQuarantine=true`
5. Test hybrid mode with both gates enabled
6. Verify QP authority overrides K8s informer correctly (when enabled)
7. Test time-based trust transitions (30s freshness, 60s staleness)
8. Verify state transitions preserve breaker capacity and refCount
9. Ensure podNotReady pods are excluded from routing
10. Test draining with active requests
11. Verify metrics are correctly reported for all modes
12. Check WebSocket compatibility if modifying proxy code

**Key Test Files:**
- `pkg/activator/net/throttler_default_test.go` - Default mode (QP authority OFF, Quarantine ON)
- `pkg/activator/net/qp_authority_test.go` - QP authority mode tests
- `pkg/activator/net/throttler_quarantine_test.go` - Quarantine-only mode tests
- `pkg/activator/net/throttler_hybrid_test.go` - Hybrid mode (both features enabled)
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
