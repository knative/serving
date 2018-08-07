# Performance tests

Knative performance tests are test geared towards producing useful performance metrics of the knative system. As such they can choose to take a blackbox point-of-view of the system and use it just like an end-user might see it. They can also go more whiteboxy to narrow down the components under test.

Existing scenarios:
- [Observed concurrency](./observed-concurrency)