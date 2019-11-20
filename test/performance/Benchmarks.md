# Benchmarks

Knative performance benchmarks are tests geared towards producing useful
performance metrics of the knative system. All the raw metrics are stored in
[mako](https://github.com/google/mako)

## Writing new benchmarks

For creating new benchmarks, follow the steps:

1. Create a new directory under `./test/performance/benchmarks`.
2. Create two benchmarks in the benchmark directory using
   [mako](https://github.com/google/mako/blob/master/docs/GUIDE.md#preparing-your-benchmark)
   as mentioned in [benchmark configs](#benchmark-configs).
3. Create a `kodata` directory and add the [four symlinks](#Benchmark-Symlinks).
4. Write a `Go` program that runs the test and stores the result in
   [mako](##Writing-to-mako)
5. (Optional)Create a
   [setup.yaml](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-setup.yaml)
   that includes all the K8S and Knative objects needed to run the test.
6. Create a
   [cron.yaml](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml)
   that defines how to run and capture metrics as mentioned in
   [benchmark cronjobs](#Benchmark-cronjobs).
7. Test and confirm the dev config works on your personal cluster.
8. Add a
   [cluster.yaml](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/cluster.yaml)
   that includes the configuration information for a GKE cluster, which will be
   created to run this benchmark. If the file is not added, a minimum cluster
   will be created.
9. Create a PR with all the changes and get it merged.
10. Once the cluster is created, the hourly job will build, push and apply all
    the updates and the SUT cronjobs will start running. The metrics can be
    viewed at [mako.dev](https://mako.dev/)

## Benchmark Configs

We will be using two
[mako benchmarks](https://github.com/google/mako/blob/master/docs/GUIDE.md#preparing-your-benchmark)
with the same config.

1. [dev.config](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/dev.config)
   This will be only used for development and testing changes and will have less
   restrictions on who can write to the benchmark.
2. [prod.config](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/prod.config)
   This will be used for looking at the state of the project and will be
   restricted to prod-only robots(and some admins).

## Benchmark Symlinks

Every benchmark directory under `/test/performance/benchmarks` has a `kodata`
directory and it should have four symlinks.

1. [dev.config](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/kodata/dev.config)
   Points to the dev.config file in the bechmark directory.

   ```sh
   ln -r -s ./test/performance/benchmarks/dataplane-probe/dev.config test/performance/benchmarks/dataplane-probe/continuous/kodata/
   ```

2. [prod.config](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/kodata/prod.config)
   Points to the prod.config file in the benchmark directory.

   ```sh
   ln -r -s ./test/performance/benchmarks/dataplane-probe/prod.config test/performance/benchmarks/dataplane-probe/continuous/kodata/
   ```

3. [HEAD](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/kodata/HEAD)
   Points to `.git/HEAD`.

   ```sh
   ln -r -s .git/HEAD test/performance/benchmarks/dataplane-probe/continuous/kodata/
   ```

4. [refs](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/kodata/HEAD)
   Points to `.git/refs`.

   ```sh
   ln -r -s .git/refs test/performance/benchmarks/dataplane-probe/continuous/kodata/
   ```

These files will be packaged with the benchmark and pushed to the test image.
The prod and dev configs define which benchmark the SUT will be writing to at
runtime. The HEAD and refs are used to get the serving commitId at which the SUT
is running.

## Benchmark CronJobs

Every benchmark will have one or more
[cronjob](https://github.com/knative/serving/blob/master/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml)
that defines how to run the benchmark SUT. In addition to the SUT container, we
need to add the following:

1. [Mako sidecar](https://github.com/knative/serving/blob/d73bb8378cab8bb0c1825aa9802bea9ea2e6cb26/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml#L71-L78)
   This allows mako to capture the metrics and write to its server.
2. [Mako Secrets volume](https://github.com/knative/serving/blob/d73bb8378cab8bb0c1825aa9802bea9ea2e6cb26/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml#L80-L82)
   Robot ACL permissions to write to mako.
3. [Config Map](https://github.com/knative/serving/blob/d73bb8378cab8bb0c1825aa9802bea9ea2e6cb26/test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml#L83-L85)
   Config map that defines which config to use at runtime.

```yaml
- name: mako
  image: gcr.io/knative-performance/mako-microservice:latest
  env:
    - name: GOOGLE_APPLICATION_CREDENTIALS
      value: /var/secret/robot.json
  volumeMounts:
    - name: mako-secrets
      mountPath: /var/secret
  volumes:
    - name: mako-secrets
      secret:
        secretName: mako-secrets
    - name: config-mako
      configMap:
        name: config-mako
```

## Writing to mako

Knative uses [mako](https://github.com/google/mako) to store all the performance
metrics. To store these metrics in the test, follow these steps:

1. Import the mako package

   ```go
   import (
     "knative.dev/pkg/test/mako"
   )
   ```

2. Create the mako client handle. By default, the mako package adds the
   commitId, environment(dev/prod) and K8S version for each run of the SUT. If
   you want to add any additional
   [tags](https://github.com/google/mako/blob/master/docs/TAGS.md), you can
   define them and pass them through
   [setup](https://github.com/knative/pkg/blob/3588ed3e5c74b25740bbc535a2a43dfac998fa8a/test/mako/sidecar.go#L178).

   ```go
   tag1 := "test"
   tag2 := "test2"
   mc, err := mako.Setup(ctx)
   defer mc.ShutDownFunc(context.Background())
   ```

3. Store metrics in
   [mako](https://github.com/google/mako/blob/master/docs/GUIDE.md)
4. Add
   [analyzers](https://github.com/google/mako/blob/master/docs/GUIDE.md#add-regression-detection)
   to analyze regressions(if any)
5. Visit [mako](https://mako.dev/project?name=Knative) to look at the benchmark
   runs

## Testing Existing Benchmarks

For testing existing benchmarks, use
[dev.md](https://github.com/knative/serving/blob/master/test/performance/dev.md)

## Admins

We currently have two admins for benchmarking with mako.

1. `[vagababov](https://github.com/vagababov)`
2. `[chizhg](https://github.com/chizhg)`
