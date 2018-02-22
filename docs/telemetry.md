# Collecting and generating telemetry

Deploy Prometheus, service monitors and Grafana:
```shell
bazel run config/prometheus:everything.create
bazel run config/grafana:everything.create
bazel run config/grafana-public:everything.create
```

To see pre-installed system dashboards, find the public IP that Grafana is exposed on:

```shell
# Put the Ingress IP into an environment variable.
$ kubectl get service grafana-public -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"
```

And browse to <IP_ADDRESS>:30802.

**Above installs Grafana with a hard coded admin username (_admin_) and password (_admin_) 
and publishes it on a public IP. This should only be done in a development environment with no sensitive data.**

## Troubleshooting

You can expose Prometheus UI to troubleshoot metric publishing or service discovery issues:

```shell
bazel run config/prometheus:everything-public.create
```

**Run this only in a development environment with no sensitive data. This step enables viewing all metrics on a public IP without any authorization.**

Wait until the load balancer IP is created and then copy the IP address:

```shell
# Put the Ingress IP into an environment variable.
$ kubectl get service prometheus-system-public -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"
```

And browse to <IP_ADDRESS>:30800.

## Generating metrics in a user apps

See [Telemetry Sample](../sample/telemetrysample/README.md) for details.

## Generating metrics in Elafros system apps

See [../pkg/controller/route/controller.go](../pkg/controller/route/controller.go) as an example for emitting metrics. 
Search for usages of "routeProcessItemCount" within the code.

See [../cmd/ela-controller/main.go](../cmd/ela-controller/main.go) as an example for setting up the endpoint for Prometheus scraping.
Search for usages of "metricsScrapeAddr" within the code.


