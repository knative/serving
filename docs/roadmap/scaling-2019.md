# 2019 Autoscaling Roadmap

## 2018 Recap

Before we get into the 2019 roadmap, here is a quick recap of what we did in 2018.

### Correctness

1. **Write autoscaler end-to-end tests**: we put a few key autoscaling end-to-end tests in place.  [AutoscaleUpDownUp](https://github.com/knative/serving/blob/51b74ba2b78b96fa4b7db3181b4a1c84c2758168/test/e2e/autoscale_test.go#L275) broadly covers scaling from N to 0 and back again.  [AutoscaleUpCountPods](https://github.com/knative/serving/blob/51b74ba2b78b96fa4b7db3181b4a1c84c2758168/test/e2e/autoscale_test.go#L327) asserts autoscaler stability and reactivity.
2. **Test error rates at high scale** TODO: STATE OF THE WORLD
3. **Test error rates around idle states**: the AutoscaleUpDownUp end-to-end test has done a good job of flushing out a variety of edge cases and errors during idle and transition states. TODO: EXAMPLES

### Performance

1. **Establish canonical load test scenarios**: TODO: STATE OF THE WORLD
2. **Reproducible load tests**:
3. **Vertical pod autoscaling**:

### Scale to Zero

1. **Implement scale to zero**:
2. **Reduce Reserve Revision start time**:

### Development

1. **Decouple autoscaling from revision controller**:

### Integration

1. **Autoscaler multitenancy**:
2. **Consume custom metrics API**:
3. **Autoscale queue-based workloads**:

## 2019 Goals

### Sub-Second Cold Start

### Streaming Autoscaling

### Overload Handling

### Vertical Pod Autoscaling Beta
