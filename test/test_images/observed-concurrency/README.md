# Observed Concurrency

## Dependencies

- General knative [development requirements](https://github.com/knative/serving/blob/master/DEVELOPMENT.md#requirements)
- [wrk](https://github.com/wg/wrk)

## Instructions to run

1. Start the application the load will go against:

```
ko apply -f app.yaml
```

2. Wait until the route is accessible

3. Run the test

```
run.sh $CONCURRENCY $DURATION

ex.

run.sh 5 60s
```

## Scenario

This test reports the *observed concurrency* from the point of view of an external user. As such, this is a blackbox test that knows nothing about the innards of knative.

The application that gets deployed is a minimal http server, that will report the server-side start and finish timestamp of a request in nanoseconds. Moreover, it will sleep for a defined amount of time (defined via a query parameter `timestamp`, which takes the time to sleep in milliseconds). The application is running in single-concurrency mode to surface any scheduling inaccuracies and to be very dependent on quick autoscaling.

The *observed concurrency* is the amount of requests that ran truly parallel at any given point in time. Firing `C` concurrent requests in parallel at that app ideally results in all of those requests starting and finishing at roughly the same time and thus being ran truly parallel. In reality, they don't as the deployment needs to be scaled appropriately first.

To gain these insights, the test will run `CONCURRENCY` parallel requests for `DURATION` amount of time. It takes all responses it got from the server (all start and finish timestamps of all requests) and then computes the observed latency at any given point in time.