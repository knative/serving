# 2018 Autoscaling Roadmap

This is what we hope to accomplish in 2018.

## References

[Autoscaling Design Goals](README.md#design-goals):

  1. *Make it fast*
  2. *Make it light*
  3. *Make everything better*

## Areas of Interest

1. **Correctness**.  When scaling from 0-to-1, 1-to-N and back down, error rates should not increase.
2. **Performance**.  When scaling from 1-to-N and back down, autoscaling should maintain reasonable latency and cost.
3. **Scale to zero**.  Idle Revisions ([Reserve](README.md#behavior)) should cost nothing.  Reserve Revisions should serve the first request in 1 second or less.
4. **Development**.  Autoscaler development should follow a clear roadmap.  Getting started as a developer should be easy and the team should scale horizontally.

### Correctness

We must have visibility of our correctness over time to make sure we've covered all the gaps and that regressions don't happen.  There are a few gaps that show up during large scaling events.  And probably some around scaling to zero.

1. **Write autoscaler conformance tests**.  This will cover low-scale regressions, runnable by individual developers before checkin.
2. **Test error rates at high scale**.  This will cover regressions at larger scales (~1000 QPS and ~1000 clients).
3. **Test error rates around idle states**.  We need to verify we handle various scale-to-zero edge cases correctly.

### Performance

The Elafros implementation of autoscaling must be competitive in its ability to serve variable load.  And to match cost to the load.

1. **Establish canonical, repeatable load test scenarios**.  We need to establish the code to run, the request load to generate, and the performance expected.  This will tell us where we need to improve.  And help us watch for performance regressions.

### Scale to Zero

1. **Reserve Revision start time**.  We need to focus on bringing the cold-start time of Revisions down from 4 seconds to 1 second or less.

### Development

1. **Custom resource definition**.  We can encapsulate the autoscaling implementation in a custom resource definition and controller.  This will let autoscaling evolve independently from the Revision custom resource controller.  This makes development more independent and scalable.
