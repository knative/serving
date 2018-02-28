# Autoscale Sample

A demonstration of the autoscaling capabilities of an Elafros Revision.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Setup

Deploy the autoscale app to Elafros from the root directory.

```shell
bazel run sample/autoscale:everything.create
```

Export your Ingress IP as SERVICE_IP.

```shell
export SERVICE_IP=`kubectl get ingress autoscale-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

Request the largest prime less than 40,000,000 from the autoscale app.  Note that it consumes about 1 cpu/sec.

```shell
time curl --header 'Host:demo.myhost.net' http://${SERVICE_IP?}/primes/40000000
```

Build the load testing container [hey](https://github.com/rakyll/hey).

```
docker build -t "${DOCKER_REPO_OVERRIDE?}/hey" sample/autoscale/hey
docker push "${DOCKER_REPO_OVERRIDE}/hey"
```

## Running

Ramp up a bunch of traffic on the autoscale app.

```shell
for i in `seq 4 4 40`; do
  kubectl -n hey run hey-$i --image "${DOCKER_REPO_OVERRIDE?}/hey" --restart Never -- \
    -n 999999 -c $i -z 3m -host 'demo.myhost.net' \
    "http://${SERVICE_IP?}/primes/40000000"
  sleep 1
done
```