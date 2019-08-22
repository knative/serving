# Benchmarks

Knative performance benchmarks are tests geared towards producing useful
performance metrics of the knative system. All the raw metrics are stored in
[mako](https://github.com/google/mako)

## Writing new benchmarks

For creating new benchmarks, follow the steps:

1. Create a new directory under `./test/performance/`.
2. Create two benchmarks using in the bechmark directory using
   [mako](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#preparing-your-benchmark)
   as mentioned in [benchmark configs](#benchmark-configs).
3. Create a `kodata` directory and add the [four symlinks](#Benchmark-Symlinks).
4. Write a go program that runs the test and stores the result in
   [mako](##Writing-to-mako)
5. Create a [setup.yaml](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe-setup.yaml)
   that lists all the K8S and Knative objects needed to run the test.
6. Create a [cron.yaml](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe.yaml)
   that defines how to run and capture metrics as mentioned in
   [benchmark cronjobs](#Benchmark-cronjobs).
7. Ask one of the [admins](#Admins) to run the `create_cluster_benchmark.sh`
   script to create a new cluster for the benchmark. Please provide the
   bencmark name and the resource requirement for running the benchmark.
8. Once the cluster is created, the hourly job will build, push and apply all
   the updates and the SUT cronjobs will start running. The metrics can be
   viewed at [mako.dev](https://mako.dev/)

## Benchmark Configs

We will be using two [mako benchmarks](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#preparing-your-benchmark)
with the same config.

1. [dev.config](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dev.config)
   This will be only used for development and testing changes and will have
   less restrictions on who can write to the benchmark.
2. [prod.config](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/prod.config)
   This will be used for looking at the state of the project and will be
   restricted to prod-only robots(and some admins).

## Benchmark Symlinks

Every benchmark directory under `/test/performance/` has a `kodata` directory
and it should have four symlinks.

1. [dev.config](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/kodata/dev.config)
   Points to the dev.config file in the bechmark directory.

   ```sh
   ln -r -s ./test/performance/dataplane-probe/dev.config test/performance/dataplane-probe/kodata/
   ```

2. [prod.config](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/kodata/prod.config)
   Points to the prod.config file in the benchmark directory.

   ```sh
   ln -r -s ./test/performance/dataplane-probe/prod.config test/performance/dataplane-probe/kodata/
   ```

3. [HEAD](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/kodata/HEAD)
   Points to `.git/HEAD`.

   ```sh
   ln -r -s .git/HEAD test/performance/dataplane-probe/kodata/
   ```

4. [refs](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/kodata/HEAD)
   Points to `.git/refs`.

   ```sh
   ln -r -s .git/refs test/performance/dataplane-probe/kodata/
   ```

These files will be packaged with the benchmark and pushed it the test image.
The prod and dev configs define which benchmark the SUT will be writing to at
runtime. The HEAD and reds are used to get the serving commitId at which the
SUT is running.

## Benchmark CronJobs

Every benchmark will have one or more
[cronjob](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe.yaml)
that defines how to run the benchmark SUT. In addition to the SUT container,
we need to add the following:

1. [Mako sidecar](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe.yaml#L38-L45)
   This allows mako to capture the metrics and write to its server.
2. [Service Account volume](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe.yaml#L47)
   Robot ACL permissions to write to mako.
3. [Config Map](https://github.com/knative/serving/blob/master/test/performance/dataplane-probe/dataplane-probe.yaml#L50)
   Config map that defines which config to use at runtime.

```yaml
- name: mako
  image: gcr.io/mattmoor-public/mako-microservice:latest
  env:
  - name: GOOGLE_APPLICATION_CREDENTIALS
    value: /var/secret/robot.json
  volumeMounts:
  - name: service-account
    mountPath: /var/secret
  volumes:
  - name: service-account
    secret:
      secretName: service-account
  - name: config-mako
    configMap:
      name: config-mako
```

## Writing to mako

Knative uses [mako](https://github.com/google/mako) to store all the
performance metrics. To store these metrics in the test, follow these steps:

1. Import the mako package

    ```go
    import (
      "knative.dev/pkg/test/mako"
    )
    ```

2. Create the mako client handle. By default, the mako package adds the
   commitId, environment(dev/prod) and K8S version for each run of the SUT. If
   you want to add any additional
   [tags](https://github.com/google/mako/blob/github-push-test-2/docs/TAGS.md),
   you can define them and pass them through
   [setup](https://github.com/knative/serving/blob/master/test/performance/mako/sidecar.go#L50).

    ```go
    tag1 := "test"
    tag2 := "test2"
    q, qclose, err := mako.Setup(ctx, tag1, tag2)
    defer qclose(context.Background())
    ```

3. Store metrics in [mako](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md)
4. Add [analyzers](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#add-regression-detection)
   to analyze regressions(if any)
5. Visit [mako](https://mako.dev/project?name=Knative) to look at the benchmark runs

## Testing Existing Benchmarks

For testing existing benchmarks, use [dev.md](https://github.com/knative/serving/blob/master/test/performance/dev.md)

## Admins

We currently have four admins for benchmarking with mako.

1. `mattmoor`
2. `vagababov`
3. `srinivashegde`
4. `chizhg`

### Creating a new cluster

Use the [create_cluster_benchmark.sh](https://github.com/knative/serving/blob/master/test/performance/tools/create_cluster_benchmark.sh)
to create a new cluster. By default, we will be using `n1-standard-4` machine
with autoscaling disabled. Please pick a zone which has enough
[quota](https://console.cloud.google.com/iam-admin/quotas?project=knative-performance&service=compute.googleapis.com&metric=CPUs).

```sh
./test/performance/tools/create_cluster_benchmark.sh --name=<dir_name> --zone=<zone> --num_nodes<num>
```
