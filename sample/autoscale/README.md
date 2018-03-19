# Autoscale Sample

A demonstration of the autoscaling capabilities of an Elafros Revision.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Setup

Deploy a simple 3D tic-tac-toe app with a little CPU and IO.

```shell
bazel run //sample/autoscale/kdt3:everything.create
```

Export your Ingress IP as SERVICE_IP (or whatever the target cluster ingress is.)

```shell
export SERVICE_IP=`kubectl get ingress autoscale-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

Hit the app to verify it's running.

```shell
time curl --header 'Host:autoscale-kdt3.myhost.net' http://${SERVICE_IP?}/game/
```

## Running

Ramp up 1000 concurrent clients.

```shell
CLIENT_COUNT=1000
RAMP_TIME_SECONDS=200
for i in `seq 10 10 $CLIENT_COUNT`; do
  kubectl run wrk-$i \
    --image josephburnett/wrk2:latest \
    --restart Never --image-pull-policy=Always -l "app=wrk" -n wrk \
    -- -c10 -t10 -d10m -R10 -a -s /wrk2/scripts/points.lua \
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

Slower rampup:

```shell
CLIENT_COUNT=1000
RAMP_TIME_SECONDS=400
```

Lower peak:

```shell
CLIENT_COUNT=100
RAMP_TIME_SECONDS=200
```

Ludicrous mode:

```shell
CLIENT_COUNT=1000
RAMP_TIME_SECONDS=100
```

## Analysis

Calculate average QPS in 10 second increments.

```shell
kubectl logs -n wrk -l "app=wrk" | awk '/===STATUS===/ { sec = 10*int($2/10); count[sec]++; } END { for (sec in count) print sec " " count[sec] / 10 }' | sort
```

Calculate average latency in 10 second increments.

```shell
kubectl logs -n wrk -l "app=wrk" | awk '/===LATENCY===/ { sec = 10*int($2/10000000); sum[sec] += $3; count[sec]++ } END { for (sec in sum) print sec " " sum[sec] / count[sec] / 1000 }' | sort
```

Calculate average error rate in 10 second increments.

```shell
kubectl logs -n wrk -l "app=wrk" | awk '/===STATUS===/ { sec = 10*int($2/10); count[sec]++; if ($3 != "200") error[sec]++ } END { for (sec in count) print sec " " error[sec] / count[sec] }' | sort
```

Calculate the total client count in 10 second increments.

```shell
kubectl logs -n wrk -l "app=wrk" | awk '/===CLIENT===/' | sort | awk '{ sec = 10*int($2/10); total++; count[sec] = total } END { for (sec in count) print sec " " count[sec] }' | sort
```

## Cleanup

```shell
kubectl delete namespace wrk
bazel run sample/autoscale-kdt3:everything.delete
```

## References

This load test uses a modified version of `wrk2`.  Source code is [here](https://github.com/josephburnett/wrk2) and can be run directly from the [Dockerhub repo](https://hub.docker.com/r/josephburnett/wrk2/).
