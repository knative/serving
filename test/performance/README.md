# Performance tests

Knative performance tests are tests geared towards producing useful performance
metrics of the knative system. As such they can choose to take a closed-box
point-of-view of the system and use it just like an end-user might see it. They
can also go more open-boxy to narrow down the components under test.

## Load Generator

Knative uses [vegeta](https://github.com/tsenart/vegeta/) to generate HTTP load.
It can be configured to generate load at a predefined rate. Officially it
supports constant rate and sine rate, but if you want to generate load at a
different rate, you can write your own pacer by implementing
[Pacer](https://github.com/tsenart/vegeta/blob/e04d9c0df8177e8633bff4afe7b39c2f3a9e7dea/lib/pacer.go#L10)
interface. Custom pacer implementations used in Knative tests are under
[pacers](https://github.com/knative/pkg/tree/main/test/vegeta/pacers).

## Benchmarking

Knative uses [mako](https://github.com/google/mako) for benchmarking. It
provides a set of tools for metrics data storage, charting, statistical
aggregation and performance regression analysis. To use it to create a benchmark
for Knative and run it continuously, please refer to
[Benchmarks.md](https://github.com/knative/serving/blob/main/test/performance/Benchmarks.md).
