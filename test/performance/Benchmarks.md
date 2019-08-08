# Benchmarks

Knative performance benchmarks are tests geared towards producing useful
performance metrics of the knative system. All the raw metrics are stored in
[mako](https://github.com/google/mako)

## Writing new benchmarks

For creating new benchmarks, follow the steps:

1. Create a new directory under `./test/performance/`.
2. Create a new [mako config](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#preparing-your-benchmark).
3. Create a new benchmark using that config using the
   [mako cli](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#preparing-your-benchmark).
4. Set up the System under test(SUT) eg. properly configured Knative resources.
5. Write a go program to run the benchmark and store results in [mako](##Writing-to-mako)
6. Create a cronjob that will run the benchmark at some frequency. Add the mako 
   microservice as a sidecar and add the robot account in the spec. The secrets and robot 
   should be mounted by the cluster creation script already 

```yaml
- name: mako
  image: us.gcr.io/mattmoor-public/mako-microservice:latest
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
```

7. Create a symlink to HEAD `ln -s -r .git/HEAD ./test/performance/<dir>/kodata/`
8. Run the [create_cluster_benchmark.sh](https://github.com/knative/serving/blob/master/test/performance/tools/create_cluster_benchmark.sh)
script as

```bash
./create_cluster_benchmark.sh --name=<dir_name> --zone=<zone> --num_nodes=<node-count>
```

## Writing to mako

Knative uses [mako](https://github.com/google/mako) to store all the
performance metrics. To store these metrics, follow these steps:

1. Import all mako libraries

    ```go
    import (
    "github.com/golang/protobuf/proto"
    "github.com/google/mako/helpers/go/quickstore"
    qpb "github.com/google/mako/helpers/proto/quickstore/quickstore_go_proto"
    )
    ```

2. Create a mako client handle.

    ```go
    q, qclose, err := quickstore.NewAtAddress(ctx, &qpb.QuickstoreInput{
      BenchmarkKey: proto.String(*benchmark),
      Tags:        tags,
      }, mako.SidecarAddress)
    defer qclose(context.Background())
    ```

3. Store metrics in [mako](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md)
4. Add [analyzers](https://github.com/google/mako/blob/github-push-test-1/docs/GUIDE.md#add-regression-detection)
   to analyze regressions(if any)
5. Visit [mako](https://mako.dev/project?name=Knative) to look at the benchmark runs
