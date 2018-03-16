# 2018 Autoscaling Roadmap

This is what we hope to accomplish in 2018.

## References

[Autoscaling Design Goals](README.md#design-goals):

  1. *Make it fast*
  2. *Make it light*
  3. *Make everything better*

In 2018 we will focus on making autoscaling correct, fast and light.  Not so much on making everything better which is a longer term endeavour.

## Areas of Interest and Requirements

1. **Correctness**.  When scaling from 0-to-1, 1-to-N and back down, error rates must not increase.  We must have visibility of correctness over time at small and large scales.
2. **Performance**.  When scaling from 1-to-N and back down, autoscaling must maintain reasonable latency and cost.  The Elafros implementation of autoscaling must be competitive in its ability to serve variable load.  And, to a lesser degree, match cost to the load.
3. **Scale to zero**.  Idle ([Reserve](README.md#behavior)) Revisions must cost nothing.  Reserve Revisions must serve the first request in 1 second or less.
4. **Development**.  Autoscaler development must follow a clear roadmap.  Getting started as a developer must be easy and the team must scale horizontally.

### Correctness

1. **Write autoscaler conformance tests** to cover low-scale regressions, runnable by individual developers before checkin.
2. **Test error rates at high scale** to cover regressions at larger scales (~1000 QPS and ~1000 clients).
3. **Test error rates around idle states** to cover various scale-to-zero edge cases.

### Performance

1. **Establish canonical load test scenarios** to prove autoscaler performance and guide development.  We need to establish the code to run, the request load to generate, and the performance expected.  This will tell us where we need to improve.
2. **Reproducable load tests** which can be run by anyone with minimal setup.  These must be transparent and easy to run.  They must be meaningful tests which prove autoscaler performance.

### Scale to Zero

1. **Reduce Reserve Revision start time** from 4 seconds to 1 second or less.  Maybe we can keep some resources around like the Replica Set or pre-cache images so that Pods are spun up faster.

### Development

1. **Custom resource definition and controller** to encapsulate the autoscaling implementation.  This will let autoscaling evolve independently from the Revision custom resource and controller.  This makes development more independent and scalable.

## What We Are Not Doing

