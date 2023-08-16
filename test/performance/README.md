# Performance tests

Knative performance tests are tests geared towards producing useful performance
metrics of the knative system. As such they can choose to take a closed-box
point-of-view of the system and use it just like an end-user might see it. They
can also go more open-box to narrow down the components under test.

## Load Generator

Knative uses [vegeta](https://github.com/tsenart/vegeta/) to generate HTTP load.
It can be configured to generate load at a predefined rate. Officially it
supports constant rate and sine rate, but if you want to generate load at a
different rate, you can write your own pacer by implementing
[Pacer](https://github.com/tsenart/vegeta/blob/e04d9c0df8177e8633bff4afe7b39c2f3a9e7dea/lib/pacer.go#L10)
interface. Custom pacer implementations used in Knative tests are under
[pacers](https://github.com/knative/pkg/tree/main/test/vegeta/pacers).


## Testing architecture



## Benchmarks

Knative Serving has different benchmarking scenarios:

* [deployment-probe](./benchmarks/deployment-probe) = Measure deployment latency
* 


## Running the benchmarks

### Development

You can run all the benchmarks directly by calling the `main()` method in `main.go` in the respective [benchmarks](./benchmarks) folders.

### On cluster




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


## Forwarding the results to Prometheus

Prerequisites
* You need a running Prometheus instance
* If you want to visualize the results, you also need Grafana and install the [dashboards](./TODO)
* For a local-setup you can use the following resources

```bash
# Setup Influx DB in local cluster
helm repo add influxdata https://helm.influxdata.com/
kubectl create ns influx
helm upgrade --install -n influx local-influx influxdata/influxdb2
echo $(kubectl get secret local-influx-influxdb2-auth -o "jsonpath={.data['admin-password']}" --namespace influx | base64 --decode)
kubectl port-forward -n influx svc/local-influx-influxdb2 8080:80
```

### Influx DB Setup

* Log in to influx UI and create an organization `Knativetest` with bucket `knative-serving`.
* Create a new user (or use admin) and grab it's token:
  * Load Data -> API Tokens -> Generate API token
  * Pass the token and URL to your tests using env variables:

```bash
export INFLUX_URL=
export INFLUX_TOKEN=xxx
```
