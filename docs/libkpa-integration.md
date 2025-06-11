# LibKPA Integration in Knative Serving

This document describes the integration of [libkpa](https://github.com/Fedosin/libkpa) - a standalone Go library extracted from Knative Serving's autoscaler (KPA - Knative Pod Autoscaler).

## Overview

The integration replaces the built-in autoscaling algorithm implementation with libkpa while maintaining full compatibility with the existing Knative Serving architecture. This allows for:

- Easier testing and development of autoscaling algorithms
- Reuse of the autoscaling logic in other projects
- Clear separation of concerns between the autoscaling algorithm and Knative-specific integrations

## Architecture

The integration is implemented through the `libkpaAutoscaler` type in `pkg/autoscaler/scaling/libkpa_autoscaler.go`, which:

1. Implements the `UniScaler` interface required by Knative Serving
2. Translates between Knative's `DeciderSpec` and libkpa's `AutoscalerSpec`
3. Collects metrics using Knative's existing metric collection infrastructure
4. Returns scaling recommendations in the format expected by Knative

## Key Components

### libkpaAutoscaler

The main adapter that bridges Knative Serving and libkpa:

```go
type libkpaAutoscaler struct {
    namespace    string
    revision     string
    metricClient kmetrics.MetricClient
    podCounter   resources.EndpointsCounter
    reporterCtx  context.Context
    mux          sync.RWMutex
    spec         *DeciderSpec
    autoscaler   *algorithm.SlidingWindowAutoscaler
}
```

### Configuration Mapping

The integration maps Knative's configuration to libkpa's configuration:

| Knative DeciderSpec | libkpa AutoscalerSpec | Notes |
|---------------------|----------------------|-------|
| MaxScaleUpRate | MaxScaleUpRate | Direct mapping |
| MaxScaleDownRate | MaxScaleDownRate | Direct mapping |
| ScalingMetric | ScalingMetric | String to enum conversion |
| TargetValue | TargetValue | Direct mapping |
| PanicThreshold | PanicThreshold | Direct mapping |
| StableWindow | StableWindow | Direct mapping |
| TargetBurstCapacity | TargetBurstCapacity | Direct mapping |

### Metric Collection

The integration uses Knative's existing metric collection infrastructure:

1. Metrics are collected by Knative's metric collectors
2. The `MetricClient` provides stable and panic window values
3. These values are packaged into a `MetricSnapshot` for libkpa
4. libkpa processes the snapshot and returns scaling recommendations

## Usage

The integration is transparent to users. The autoscaler will automatically use libkpa for scaling decisions. All existing Knative Serving configurations and annotations continue to work as before.

## Testing

The integration includes comprehensive tests in `pkg/autoscaler/scaling/libkpa_autoscaler_test.go` that verify:

- Scale up scenarios
- Scale down scenarios with rate limiting
- Activation scale behavior
- Handling of missing metrics
- Configuration updates

Run tests with:

```bash
go test ./pkg/autoscaler/scaling -run TestLibKPA
```

## Benefits

1. **Modularity**: The autoscaling algorithm is now a separate, reusable library
2. **Maintainability**: Algorithm improvements can be made in libkpa and benefit all users
3. **Testing**: Easier to test the algorithm in isolation
4. **Compatibility**: Full backward compatibility with existing Knative Serving deployments

## Future Improvements

- Add support for custom autoscaling algorithms through libkpa's plugin system
- Expose more libkpa configuration options through Knative annotations
- Add metrics about libkpa's internal state for better observability
