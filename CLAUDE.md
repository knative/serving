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

### Pod State Management (Queue-Proxy Authoritative Model)

The activator implements a simplified 4-state pod state machine where queue-proxy readiness signals are authoritative over K8s informer updates.

**Pod States:**
- `podReady (0)`: Pod is ready to serve traffic (QP confirmed readiness)
- `podDraining (1)`: Pod is being removed from service, draining active requests
- `podRemoved (2)`: Pod completely removed from tracker (terminal state)
- `podPending (3)`: Pod waiting for ready signal (NOT viable for routing)

**REMOVED:** `podQuarantined`, `podRecovering` (quarantine system removed, trust backend manager probing)

#### State Transition Map

**State Flow:**
```
NEW POD → podPending → podReady → podDraining → podRemoved
            ↓            ↓
            └────────────┘
           (ready/not-ready cycle)
```

**From podPending (new pods waiting for ready signal):**
- ✅ QP "ready" event → `podReady` (immediate, no health check)
- ✅ K8s informer "healthy" → `podReady` (if QP hasn't said "not-ready" recently)
- 🗑️ K8s informer removed → `podRemoved` (cleanup)
- 🗑️ QP "draining" event → `podDraining` (crash before ready)

**From podReady (established ready pods):**
- ⚠️ QP "not-ready" event → `podPending` (preserves refCount/breaker, stops NEW traffic)
- 🗑️ QP "draining" event → `podDraining` (graceful shutdown)
- 🗑️ K8s informer removed → `podDraining` (if QP data stale >60s)
- ✅ K8s informer "healthy" → STAYS `podReady` (confirmation)

**From podDraining (graceful shutdown):**
- ⏱️ RefCount reaches 0 → `podRemoved` (all requests complete)
- ⏱️ Stuck for maxDrainingDuration → `podRemoved` (force remove)
- 🔄 Re-added to endpoints → `podPending` (treat as new pod)

**From podRemoved (terminal state):**
- 🔄 Re-added to endpoints → `podPending` (treat as new pod)

#### Queue-Proxy Authority Model

**Trust Hierarchy:**
1. **Queue-Proxy Push (AUTHORITATIVE)** - Pod knows its own state best
   - QP data < 30s old: QP state overrides K8s informer
   - QP events: "startup", "ready", "not-ready", "draining"

2. **K8s Informer (FALLBACK)** - Trusted when QP silent
   - QP data > 60s old: Trust informer (QP likely dead/crashed)
   - QP never heard from: Informer is sole authority

**QP Events:**
- `startup`: Creates podPending tracker (not viable for traffic)
- `ready`: Promotes podPending → podReady (now viable)
- `not-ready`: Demotes podReady → podPending (stops new traffic, preserves active requests)
- `draining`: Transitions podReady → podDraining (graceful shutdown)

**Informer Override Rules:**
- ✅ QP "not-ready" < 30s ago → Informer "healthy" IGNORED
- ✅ QP "ready" < 30s ago → Informer "draining" IGNORED
- ✅ QP silent > 60s → Informer is AUTHORITATIVE (QP likely dead)
- ✅ No QP data → Informer is AUTHORITATIVE

**Critical Invariants:**
- podPending pods NEVER receive traffic (excluded from routing)
- State transitions preserve breaker capacity and refCount
- Active requests complete even when pod demoted to pending
- Only atomic CAS operations for state transitions
- No health checks in request path (trust backend manager + QP)

### Capacity Calculation

- Capacity based on ALL trackers (podReady + podPending)
- Filtering at routing time excludes podPending pods
- Prevents capacity starvation while pods are starting up
- Dynamic adjustment based on QP readiness signals

## Known Issues & TODOs

See `ACTIVATOR_FIXES.md` for detailed list of known issues.

**Recent Changes:**
- ✅ Quarantine system removed (commits da65d8228, ff6ef96aa)
- ✅ Health checks removed from request path (commit da65d8228)
- ✅ QP authoritative state model implemented (commits b94f5b134, a434d10b9)
- ✅ Comprehensive test coverage added (commit 20208ea9e)

## Testing Considerations

When making changes:

1. Run unit tests with race detection: `go test -race ./pkg/activator/net`
2. Test QP event sequences (startup, ready, not-ready, draining)
3. Verify QP authority overrides K8s informer correctly
4. Test time-based trust transitions (30s freshness, 60s staleness)
5. Verify state transitions preserve breaker capacity and refCount
6. Ensure podPending pods are excluded from routing
7. Test draining with active requests
8. Verify metrics are correctly reported
9. Check WebSocket compatibility if modifying proxy code

**Key Test Files:**
- `pkg/activator/net/qp_authority_test.go` - QP authority and state transitions
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
