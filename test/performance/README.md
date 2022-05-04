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

## Benchmarking using Mako

The benchmarks were originally built to use [mako](https://github.com/google/mako), but currently
running without connecting to the Mako backend, and collecting the data using
a Mako sidecar stub.

### Run without Mako

To run a benchmark once, and use the result from `mako-stub` for plotting:

1. Start the benchmarking job:

   `ko apply -f test/performance/benchmarks/deployment-probe/continuous/benchmark-direct.yaml`

1. Wait until all the pods with the selector equal to the job name are completed.

1. Retrieve results from mako-stub using the script in where `pod_name` is the name of the pod from the previous step.

  `read_results.sh "$pod_name" "$pod_namespace" ${mako_port:-10001} ${timeout:-120} ${retries:-100} ${retries_interval:-10} "$output_file"`

   This will download a CSV with all raw results. Alternatively you can remove
   the port argument `-p` in `mako-stub` container to dump the output to
   container log directly.

**Note:** Running `performance-tests-mako.sh` creates a cluster and runs all the benchmarks in sequence. Results are downloaded in a temp folder

### Benchmarking using Kperf

Running `performance-tests.sh` runs performance tests using [kperf](https://github.com/knative-sandbox/kperf)
