# Queue-Proxy Authoritative State - IMPLEMENTED ✅

**Status:** Fully implemented across commits ff6ef96aa through 20208ea9e
**Documentation:** This file serves as architecture reference for the implemented system

---

## Problem Statement

**Current issue:**
- `podPending` pods are included in viable routing capacity
- When traffic hits them, activator does a health check
- Failed health checks → immediate quarantine → unnecessary delay
- This defeats the purpose of push-based discovery (adds latency instead of removing it)

**Root cause:** Redundant health checking at two layers:
1. Revision backend manager (prober) - already probes backends
2. Request path (throttler) - redundantly checks again on first request

---

## Architectural Changes

### **New State Machine (4 states only):**
```go
const (
    podReady    podState = iota  // Ready to serve traffic (QP confirmed readiness)
    podDraining                  // Gracefully shutting down
    podRemoved                   // Deleted from tracker
    podPending                   // Waiting for ready signal (NOT viable for routing)
)
```

**REMOVED:** `podQuarantined`, `podRecovering` (and all associated quarantine logic)

---

## Core Principles

1. **Queue-Proxy is AUTHORITATIVE** - Trust QP state over K8s informer
2. **Time-based informer override** - Only trust informer if QP silent > 60s
3. **podPending is NOT viable** - Won't receive traffic until promoted to healthy
4. **No health checks in request path** - Remove all `tcpPingCheck` calls
5. **Breaker/refCount preserved** - State transitions don't reset capacity or active requests

---

## Trust Hierarchy

### **Priority Order:**
1. **Queue-Proxy Push (HIGHEST)** - Pod knows its own state best
   - "ready" → podReady (cannot be overridden by informer)
   - "not-ready" → podPending (cannot be overridden by informer saying healthy)
   - "draining" → podDraining (cannot be overridden)

2. **K8s Informer (LOWER)** - Can promote, but respects QP freshness
   - Can promote podPending → podReady (if QP hasn't recently said "not-ready")
   - Can initiate draining (if QP hasn't recently said "ready")
   - **CANNOT** demote podReady if QP recently confirmed ready

### **Time-Based Override Rules:**
- **QP data < 30s old:** QP state is authoritative, ignore informer
- **QP data > 60s old:** Trust informer (QP likely dead/crashed)
- **QP never heard from:** Informer is authoritative

---

## Changes Required

### **1. Add QP Freshness Tracking to podTracker**

**File:** `pkg/activator/net/throttler.go:111-135`

**Add fields:**
```go
type podTracker struct {
    // ... existing fields ...

    // Queue-proxy push tracking (for trust hierarchy)
    lastQPUpdate atomic.Int64  // Unix timestamp of last QP push (0 if never)
    lastQPState  atomic.Value  // Last QP event: "startup", "ready", "not-ready", "draining"
}
```

**Initialize in newPodTracker:**
```go
tracker.lastQPUpdate.Store(0)
tracker.lastQPState.Store("")
```

---

### **2. Fix Reserve() - Exclude podPending**

**File:** `pkg/activator/net/throttler.go:215-254`

**Current:**
```go
if state != podReady && state != podPending && state != podRecovering {
    p.releaseRef()
    return nil, false
}
```

**Change to:**
```go
// ONLY healthy pods can accept new requests
// This ensures podPending pods don't receive traffic prematurely
if state != podReady {
    p.releaseRef()
    return nil, false
}
```

**Impact:** podPending pods won't receive traffic, eliminating premature health check failures

---

### **3. Simplify filterAvailableTrackers - Remove Quarantine Logic**

**File:** `pkg/activator/net/throttler.go:406-444`

**Current:** 40 lines checking quarantine timers, recovery states, etc.

**Replace with:**
```go
// filterAvailableTrackers returns only healthy pods
// podPending pods are excluded from routing until explicitly promoted to healthy
func (rt *revisionThrottler) filterAvailableTrackers(trackers []*podTracker) []*podTracker {
    available := make([]*podTracker, 0, len(trackers))

    for _, tracker := range trackers {
        if tracker != nil && podState(tracker.state.Load()) == podReady {
            available = append(available, tracker)
        }
    }

    return available
}
```

**Lines removed:** ~35 (quarantine checking, recovery, expired quarantine handling)

---

### **4. Remove Health Check from Request Path**

**File:** `pkg/activator/net/throttler.go:747-823`

**Delete entire section:** ~75 lines of:
- `tcpPingCheck()` call
- Failed health check → quarantine transitions
- Successful health check → pending promotion
- Quick 502 quarantine logic (commented out but still present)
- All quarantine metrics calls

**Replace with simple execution:**
```go
// Trust that pods in podReady state are actually healthy
// Backend manager validates health via probing
ret = function(tracker.dest, isClusterIP)

// Handle successful request completion
if ret == nil {
    // Request succeeded - pod is confirmed working
    // No state changes needed, pod is already healthy
}
```

---

### **5. Update addPodIncremental - Handle All QP Events**

**File:** `pkg/activator/net/throttler.go:1474-1550`

**Add QP tracking and handle 4 event types:**
```go
func (rt *revisionThrottler) addPodIncremental(podIP string, eventType string, logger *zap.SugaredLogger) {
    rt.mux.Lock()

    existing, exists := rt.podTrackers[podIP]

    if exists {
        // Update QP freshness tracking
        existing.lastQPUpdate.Store(time.Now().Unix())
        existing.lastQPState.Store(eventType)

        currentState := podState(existing.state.Load())

        switch eventType {
        case "startup":
            // Duplicate startup - noop
            logger.Debugw("Duplicate startup event",
                "pod-ip", podIP,
                "current-state", currentState)

        case "ready":
            // QP says ready - promote to healthy immediately
            if currentState == podPending {
                if existing.state.CompareAndSwap(uint32(podPending), uint32(podReady)) {
                    logger.Infow("QP promoted pod to healthy",
                        "pod-ip", podIP,
                        "revision", rt.revID.String())
                }
            } else {
                logger.Debugw("Duplicate ready event",
                    "pod-ip", podIP,
                    "current-state", currentState)
            }

        case "not-ready":
            // QP says not-ready - demote to pending
            // CRITICAL: Preserves breaker and refCount - just stops new traffic
            if currentState == podReady {
                if existing.state.CompareAndSwap(uint32(podReady), uint32(podPending)) {
                    logger.Warnw("QP demoted pod to pending (readiness probe failed)",
                        "pod-ip", podIP,
                        "revision", rt.revID.String(),
                        "active-requests", existing.refCount.Load())
                }
            }

        case "draining":
            // QP says draining - transition to draining state
            if existing.tryDrain() {
                logger.Infow("QP initiated pod draining",
                    "pod-ip", podIP,
                    "revision", rt.revID.String(),
                    "active-requests", existing.refCount.Load())
            } else {
                logger.Debugw("Pod already in draining/removed state",
                    "pod-ip", podIP,
                    "current-state", currentState)
            }
        }

        rt.mux.Unlock()
        return
    }

    // New pod - create tracker
    // ... existing code, but also initialize QP tracking
    tracker.lastQPUpdate.Store(time.Now().Unix())
    tracker.lastQPState.Store(eventType)
}
```

---

### **6. Informer Respects QP Authority with Time-Based Override**

**File:** `pkg/activator/net/throttler.go:1195-1267` (updateThrottlerState)

**Add constants at top of file:**
```go
const (
    // QPFreshnessWindow - QP data fresher than this overrides K8s informer
    QPFreshnessWindow = 30 * time.Second

    // QPStalenessThreshold - QP older than this, trust K8s instead (QP likely dead)
    QPStalenessThreshold = 60 * time.Second
)
```

**Rewrite healthyDests handling:**
```go
for _, d := range healthyDests {
    tracker := rt.podTrackers[d]
    if tracker != nil {
        currentState := podState(tracker.state.Load())

        // Check QP freshness
        lastQPSeen := tracker.lastQPUpdate.Load()
        qpAge := time.Now().Unix() - lastQPSeen
        lastQPEvent := ""
        if val := tracker.lastQPState.Load(); val != nil {
            if s, ok := val.(string); ok {
                lastQPEvent = s
            }
        }

        switch currentState {
        case podPending:
            // K8s says healthy, pod is pending
            // Only promote if QP hasn't recently said "not-ready"
            if lastQPEvent == "not-ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
                rt.logger.Debugw("Ignoring K8s healthy - QP recently said not-ready",
                    "dest", d,
                    "qp-age-sec", qpAge,
                    "qp-last-event", lastQPEvent)
                continue
            }

            // Safe to promote (QP hasn't objected recently)
            if tracker.state.CompareAndSwap(uint32(podPending), uint32(podReady)) {
                rt.logger.Infow("K8s informer promoted pending pod to healthy", "dest", d)
            }

        case podReady:
            // Already healthy - nothing to do
            // Both K8s and current state agree

        case podDraining:
            // Pod is draining - IGNORE K8s "healthy" signal
            // Draining state takes priority
            rt.logger.Debugw("Ignoring K8s healthy signal for draining pod", "dest", d)
        }
    }
}
```

**Rewrite drainingDests handling:**
```go
for _, d := range drainingDests {
    tracker := rt.podTrackers[d]
    if tracker == nil {
        continue
    }

    currentState := podState(tracker.state.Load())
    lastQPSeen := tracker.lastQPUpdate.Load()
    qpAge := time.Now().Unix() - lastQPSeen
    lastQPEvent := ""
    if val := tracker.lastQPState.Load(); val != nil {
        if s, ok := val.(string); ok {
            lastQPEvent = s
        }
    }

    // CRITICAL: If QP recently said "ready", K8s might be stale
    // Don't drain a pod that QP confirms is healthy
    if currentState == podReady && lastQPEvent == "ready" && qpAge < int64(QPFreshnessWindow.Seconds()) {
        rt.logger.Warnw("Ignoring K8s draining signal - QP recently confirmed ready",
            "dest", d,
            "qp-age-sec", qpAge,
            "qp-last-event", lastQPEvent)
        continue
    }

    // If QP has been silent for > 60s and K8s says draining, trust K8s
    // (QP likely crashed/dead, can't self-report)
    if qpAge > int64(QPStalenessThreshold.Seconds()) || lastQPSeen == 0 {
        rt.logger.Infow("QP silent for long time - trusting K8s informer to drain pod",
            "dest", d,
            "qp-age-sec", qpAge,
            "qp-last-event", lastQPEvent)
    }

    // Proceed with draining
    switch currentState {
    case podReady:
        if tracker.tryDrain() {
            rt.logger.Debugw("K8s informer initiated draining", "dest", d, "refCount", tracker.refCount.Load())
        }

    case podPending:
        // Pending pod being removed - just delete it immediately
        tracker.state.Store(uint32(podRemoved))
        delete(rt.podTrackers, d)
        rt.logger.Debugw("Removed pending pod", "dest", d)

    case podDraining:
        // Already draining - handle refCount cleanup
        refCount := tracker.refCount.Load()
        if refCount == 0 {
            tracker.state.Store(uint32(podRemoved))
            delete(rt.podTrackers, d)
            rt.logger.Debugw("Removed drained pod with no active requests", "dest", d)
        } else {
            // Check for stuck draining pods
            drainingStart := tracker.drainingStartTime.Load()
            if drainingStart > 0 && time.Now().Unix()-drainingStart > int64(maxDrainingDuration.Seconds()) {
                rt.logger.Warnf("Force removing pod stuck in draining for too long",
                    "dest", d, "refCount", refCount)
                tracker.state.Store(uint32(podRemoved))
                delete(rt.podTrackers, d)
            }
        }
    }
}
```

---

### **7. Queue-Proxy Sends Readiness State Changes**

**File:** `pkg/queue/sharedmain/main.go:246-259`

**Replace current probe wrapper:**
```go
// Track probe state to detect transitions
var lastProbeResult atomic.Bool
lastProbeResult.Store(false)  // Start as not-ready

probe = func() bool {
    result := originalProbe()
    previous := lastProbeResult.Swap(result)

    // Detect state transition: not-ready → ready
    if result && !previous {
        if readyRegistrationSent.CompareAndSwap(false, true) {
            logger.Infow("Readiness probe passed - notifying activator",
                "pod", env.ServingPod,
                "pod-ip", env.ServingPodIP)
            queue.RegisterPodWithActivator(env.ActivatorServiceURL, queue.EventTypeReady,
                env.ServingPod, env.ServingPodIP, env.ServingNamespace, env.ServingRevision, logger)
        }
    }

    // Detect state transition: ready → not-ready
    if !result && previous {
        logger.Warnw("Readiness probe failed - notifying activator",
            "pod", env.ServingPod,
            "pod-ip", env.ServingPodIP)
        queue.RegisterPodWithActivator(env.ActivatorServiceURL, queue.EventTypeNotReady,
            env.ServingPod, env.ServingPodIP, env.ServingNamespace, env.ServingRevision, logger)
    }

    return result
}
```

---

### **8. Add Draining Event on Shutdown**

**File:** `pkg/queue/sharedmain/main.go:136-140` (in shutdown handler)

**Add before drain:**
```go
case <-d.Ctx.Done():
    logger.Info("Received TERM signal, attempting to gracefully shutdown servers.")

    // Notify activator we're draining BEFORE starting drain
    // This gives activator immediate signal to stop routing traffic here
    logger.Infow("Notifying activator of pod draining",
        "pod", env.ServingPod,
        "pod-ip", env.ServingPodIP)
    queue.RegisterPodWithActivator(env.ActivatorServiceURL, queue.EventTypeDraining,
        env.ServingPod, env.ServingPodIP, env.ServingNamespace, env.ServingRevision, logger)

    drainer.Drain()
```

---

### **9. Add New Event Type Constants**

**File:** `pkg/queue/pod_registration.go:42-46`

**Add:**
```go
// EventTypeNotReady indicates the pod's readiness probe failed
EventTypeNotReady = "not-ready"

// EventTypeDraining indicates the pod is shutting down
EventTypeDraining = "draining"
```

---

### **10. Update Event Type Validation**

**File:** `pkg/activator/net/pod_registration_handler.go:73-80`

**Update validation:**
```go
// Validate event type is one of the expected values
validEvents := map[string]bool{
    "startup":    true,
    "ready":      true,
    "not-ready":  true,
    "draining":   true,
}
if !validEvents[req.EventType] {
    logger.Warnw("Invalid event type",
        "event_type", req.EventType,
        "pod_name", req.PodName,
        "pod_ip", req.PodIP)
    http.Error(w, "Invalid event_type: must be 'startup', 'ready', 'not-ready', or 'draining'", http.StatusBadRequest)
    return
}
```

---

### **11. Remove ALL Quarantine Code**

**File:** `pkg/activator/net/throttler.go`

**Delete these complete functions:**
- Lines 383-404: `transitionOutOfQuarantine()` function
- Lines 372-405: `quarantineBackoffSeconds()` function

**Delete from podState enum:**
- Line 105: `podQuarantined`
- Line 106: `podRecovering`

**Delete from podTracker struct:**
```go
quarantineEndTime atomic.Int64
quarantineCount atomic.Uint32
```

**Remove all calls to quarantine metrics:**
- `handler.RecordPodQuarantineChange()`
- `handler.RecordPodQuarantineEntry()`
- `handler.RecordPodQuarantineExit()`
- `handler.RecordTCPPingFailureEvent()`
- `handler.RecordImmediate502Event()`

**Update all switch/case statements:**
- Remove cases for `podQuarantined`, `podRecovering`
- Update state machine transitions to only use 4 states

**Delete quarantine cleanup in updateThrottlerState:**
- Lines 1251-1258: Quarantine removal during draining
- Lines 1269-1301: Proactive quarantine cleanup

**Delete health check functions:**
- Lines 310-370: `podReadyCheck()` function
- Lines 407-410: `tcpPingCheck()` wrapper
- Line 312: `podReadyCheckFunc` atomic.Value
- Lines 314-316: init() that sets podReadyCheckFunc
- Lines 412-414: `setPodReadyCheckFunc()` (only used in tests)

---

### **12. Update updateThrottlerState - Handle QP Authority**

**File:** `pkg/activator/net/throttler.go:1195-1302`

**Add QP-aware logic to healthyDests and drainingDests sections** (see section 6 above)

---

### **13. Simplify State Transition Comments**

**File:** `CLAUDE.md` (update documentation)

**Update state transition map to reflect 4-state model:**
```
podReady → podDraining → podRemoved
          ↗             ↗
podPending ---------------
```

**Remove quarantine/recovery documentation**

---

## Testing Strategy

### **Unit Tests to Add:**

1. **Test QP authority over informer:**
   - QP says "ready", informer says "not ready" → pod stays healthy
   - QP says "not-ready", informer says "healthy" → pod stays pending

2. **Test time-based override:**
   - QP silent < 30s: Informer ignored
   - QP silent > 60s: Informer trusted

3. **Test all 4 QP events:**
   - startup → creates podPending
   - ready → promotes to podReady
   - not-ready → demotes to podPending
   - draining → transitions to podDraining

4. **Test state preservation:**
   - podReady → podPending doesn't reset breaker
   - Active requests complete during demotion

### **Tests to Remove:**
- All quarantine-related tests
- Health check timeout tests
- Quarantine backoff tests

---

## Migration Impact

### **Breaking Changes:**
- ❌ Removes quarantine metrics (users relying on these will need to adapt)
- ❌ Removes request-path health checking (trust backend manager instead)
- ❌ Changes capacity calculation (podPending excluded)

### **Behavioral Changes:**
- ✅ Faster cold starts (no health check on first request)
- ✅ QP-driven state takes priority
- ✅ Explicit not-ready handling
- ✅ Faster draining (QP controls timing)

---

## Files Affected

### **Core Changes:**

1. **pkg/activator/net/throttler.go** (~250 lines removed, ~80 added)
   - Remove quarantine states and all logic
   - Add QP tracking fields
   - Fix `Reserve()` to exclude podPending
   - Remove health check from request path
   - Add time-based informer override

2. **pkg/queue/sharedmain/main.go** (~25 lines added)
   - Send "not-ready" on probe state change
   - Send "draining" on shutdown

3. **pkg/queue/pod_registration.go** (~5 lines added)
   - Add EventTypeNotReady, EventTypeDraining constants

4. **pkg/activator/net/pod_registration_handler.go** (~10 lines modified)
   - Update event type validation to include new events

5. **CLAUDE.md** (documentation update)
   - Update state machine to 4-state model
   - Remove quarantine documentation

### **Test Updates:**

6. **pkg/activator/net/throttler_test.go** (~50 lines removed)
   - Remove quarantine tests
   - Update capacity tests to exclude podPending

7. **pkg/activator/net/pod_registration_handler_test.go** (~30 lines added)
   - Add tests for "not-ready" event
   - Add tests for "draining" event
   - Test QP authority over informer

---

## Net Result

- **Lines removed:** ~250 (quarantine, health checks, recovery)
- **Lines added:** ~120 (QP tracking, draining, not-ready, time-based logic)
- **Net reduction:** ~130 lines
- **States:** 6 → 4 (simpler)
- **Trust model:** Single authoritative source (QP + time-based informer fallback)

---

## Success Criteria

✅ podPending pods don't receive traffic
✅ QP "ready" immediately promotes to healthy
✅ QP "not-ready" demotes without breaking active requests
✅ QP "draining" stops new traffic immediately
✅ Informer can override only when QP is stale (>60s)
✅ No health checks in request path
✅ No quarantine delays
✅ All tests pass

---

## Implementation Order

1. Add QP tracking fields to podTracker
2. Add event type constants (not-ready, draining)
3. Fix Reserve() to exclude podPending
4. Simplify filterAvailableTrackers (remove quarantine)
5. Remove health check from request path (~75 lines)
6. Update addPodIncremental to handle 4 events + QP tracking
7. Update informer logic with time-based QP override
8. Remove all quarantine code (~150 lines)
9. Update QP probe wrapper to send not-ready
10. Add draining event on shutdown
11. Update validation
12. Update tests
13. Update documentation

---

**Status:** Ready for implementation
