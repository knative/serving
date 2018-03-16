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

### Performance

### Scale to Zero

### Development
