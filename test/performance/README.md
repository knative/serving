# Performance tests

Knative performance tests are tests geared towards producing useful performance
metrics of the knative system. As such they can choose to take a blackbox
point-of-view of the system and use it just like an end-user might see it. They
can also go more whiteboxy to narrow down the components under test.

## Load Generator

Knative uses [vegeta](https://github.com/tsenart/vegeta) to generate HTTP load.
It can be configured to generate load at a predefined rate. Officially it
supports constant rate and sine rate, but if you want to generate load at a
different rate, you can write your own pacer by implementing
[Pacer](https://github.com/tsenart/vegeta/blob/ab06ddb56e2f6097bba8c5a6d168621088867949/lib/pacer.go#L13)
interface. Custom pacer implementations used in Knative tests are under
[pacers](https://github.com/knative/pkg/tree/master/test/vegeta/pacers).

## Benchmarking

Knative uses [mako](https://github.com/google/mako) for benchmarking. It
provides a set of tools for metrics data storage, charting, statistical
aggregation and performance regression analysis. To use it to create a benchmark
for Knative and run it continuously, please refer to
[Benchmarks.md](https://github.com/knative/serving/blob/master/test/performance/Benchmarks.md).
