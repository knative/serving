# Autoscale Sample

A demonstration of the autoscaling capabilities of an Elafros Revision.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)
3. [Setup telemetry](../../docs/telemetry.md)

## Setup

Deploy a simple 3D tic-tac-toe webapp.

```shell
bazel run //sample/autoscale/kdt3:everything.create
```

Export `SERVICE_HOST` and `INGRESS_IP`.

```shell
export SERVICE_HOST=`kubectl get route autoscale-kdt3-route -o jsonpath="{.status.domain}"`
export SERVICE_IP=`kubectl get ingress autoscale-kdt3-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

Verify the app is running.

```shell
time curl -I -s --header "Host:${SERVICE_HOST}" http://${SERVICE_IP?}/game/
```

## Running a QPS load test

Ramp up over 1 minute to 100 queries-per-second (qps) for 10 minutes.

```shell
CLIENT_COUNT=100
RAMP_TIME_SECONDS=60
kubectl create namespace wrk
for i in `seq 10 10 $CLIENT_COUNT`; do
  kubectl run wrk-$i \
    --image josephburnett/wrk2:latest \
    --restart Never --image-pull-policy=Always -n wrk \
    -- -c10 -t10 -d10m -R10 \
       -H 'Host: autoscale-kdt3.myhost.net' \
       "http://${SERVICE_IP}/game/"
  sleep $(( $RAMP_TIME_SECONDS / ($CLIENT_COUNT / 10) ))
done
```

Watch the Elafros deployment pod count increase.  Then return to 1.

```shell
watch kubectl get deploy
```

### Other test scenarios

Cpu heavy app:

```
bazel run //sample/autoscale/kdt3:everything.create
export SERVICE_HOST=`kubectl get route autoscale-prime-route -o jsonpath="{.status.domain}"`
export SERVICE_IP=`kubectl get ingress autoscale-prime-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
time curl -I -s --header "Host:${SERVICE_HOST}" http://${SERVICE_IP?}/primes/40000
```

Slower rampup:

```shell
CLIENT_COUNT=100
RAMP_TIME_SECONDS=400
```

Lower peak:

```shell
CLIENT_COUNT=50
RAMP_TIME_SECONDS=60
```

Ludicrous mode:

```shell
CLIENT_COUNT=1000
RAMP_TIME_SECONDS=30
```

## Analysis

Calculate average QPS in 10 second increments.
Calculate average latency in 10 second increments.
Calculate average error rate in 10 second increments.
Calculate the total client count in 10 second increments.

TODO: analysis with Elafros telemetry system.

## Running a batch job load test

Send 1000 requests, each of which consumes about 1 cpu/sec.

```shell
batch () {
  for i in `seq 1 1000`
  do
    sleep 0.02
    curl -s -o /dev/null -w "%{http_code}\n" \
      --header 'Host:autoscale.myhost.net' \
      http://${SERVICE_IP}/primes/40000000 &
  done
  wait
}
time batch 2>/dev/null | sort | uniq -c
```

## Cleanup

```shell
kubectl delete namespace wrk
bazel run sample/autoscale-kdt3:everything.delete
bazel run sample/autoscale:everything.delete
```

## References

This load test uses a modified version of `wrk2`.  Source code is [here](https://github.com/josephburnett/wrk2) and can be run directly from the [Dockerhub repo](https://hub.docker.com/r/josephburnett/wrk2/).
