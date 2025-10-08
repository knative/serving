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

### Pod State Management

The fork implements a sophisticated pod state machine with six states:

- `podHealthy (0)`: Pod is healthy and serving traffic normally
- `podDraining (1)`: Pod is being removed from service, draining active requests
- `podQuarantined (2)`: Pod failed health check, in timed backoff period
- `podRecovering (3)`: Post-quarantine probation, needs one successful request to be trusted again
- `podRemoved (4)`: Pod completely removed from tracker (terminal state)
- `podPending (5)`: Brand new pod, not yet health-checked

#### Complete State Transition Map

**Initial State:**
```
NEW POD → podPending (until first health check)
```

**From podPending (brand new pods):**
- ✅ Health check passes → `podHealthy`
- ❌ 1st health check fails → `podQuarantined` (0s backoff - immediate retry)
- ❌ 2nd failure → `podQuarantined` (1s backoff)
- ❌ 3rd failure → `podQuarantined` (1s backoff)
- ❌ 4th failure → `podQuarantined` (2s backoff)
- ❌ 5+ failures → `podQuarantined` (5s backoff, capped)
- 🗑️ Removed from endpoints → `podRemoved`
- ⚠️ Endpoint update → STAYS `podPending` (no disruption)

**From podHealthy (established healthy pods):**
- ❌ Health check fails → `podQuarantined` (conservative backoff: 1s, 2s, 5s, 10s, 20s)
- 🗑️ Removed from endpoints → `podDraining` → `podRemoved`
- ⚠️ Endpoint update → STAYS `podHealthy` (no action needed)

**From podQuarantined (in backoff period):**
- ⏱️ Quarantine timer expires → `podRecovering`
- 🗑️ Removed from endpoints → `podRemoved`
- ⚠️ Endpoint update → STAYS `podQuarantined` (respects backoff timer)

**From podRecovering (probation state):**
- ✅ Successful request served → `podHealthy` (clears quarantine count)
- ❌ Health check fails → `podQuarantined` (appropriate backoff based on history)
- 🗑️ Removed from endpoints → `podDraining` → `podRemoved`
- ⚠️ Endpoint update → STAYS `podRecovering` (no disruption)

**From podDraining (graceful shutdown):**
- ⏱️ RefCount reaches 0 → `podRemoved`
- ⏱️ Stuck for maxDrainingDuration → `podRemoved` (force remove)
- 🔄 Re-added to endpoints → `podPending` (re-validate before trusting)

**From podRemoved (terminal state):**
- 🔄 Re-added to endpoints → `podPending` (treat as new pod)

**Critical Invariants:**
- Quarantine backoff is never short-circuited by endpoint updates
- Recovering pods must serve one successful request before becoming healthy
- Pending pods always get health-checked before accepting traffic
- Only atomic CAS operations for critical state transitions
- Quarantine metrics properly incremented/decremented
- Endpoint updates respect in-progress recovery mechanisms

### Quarantine System

- Pods failing health checks are quarantined with exponential backoff
- Automatic recovery with health verification
- Maximum quarantine count before permanent eviction

### Capacity Calculation

- Tracks "effective capacity" considering pod health states
- Prevents capacity starvation from unhealthy pods
- Dynamic adjustment based on real-time health metrics

## Known Issues & TODOs

See `ACTIVATOR_FIXES.md` for detailed list of known issues including:

1. Error handling in ServeHTTP needs improvement
2. Quarantine recovery mechanism requires health verification
3. Race condition in resetTrackers() method
4. Need for pod eviction after persistent quarantines
5. ErrRequestQueueFull investigation needed

## Testing Considerations

When making changes:

1. Run unit tests with race detection: `go test -race`
2. Test pod health transitions under load
3. Verify metrics are correctly reported
4. Check WebSocket compatibility if modifying proxy code
5. Ensure error propagation works end-to-end

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
