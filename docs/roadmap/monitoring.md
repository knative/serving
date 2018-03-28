# 2018 Roadmap for Monitoring and Logging

This document captures what we hope to accomplish in 2018 in Monitoring and Logging areas for Elafros.

## Capabilities
Provide two distinct experiences - one catered towards [operator persona](../product/personas.md#operator-personas) and other towards [developer persona](../product/personas.md#developer-personas). As a general rule of thumb, we will make sure to collaborate and align with Kubernetes team on their roadmap in monitoring and logging areas.

### For the operator persona
* View logs and metrics related to cluster operations and health
* Provide default out of the box logs, metrics, alerts and dashboards
* Allow creating custom alerting rules to get notified for cluster health issues
* Auto-scale logging, metrics, alerting and tracing backends
* Allow fine tuning of scale, performance and features of logging, metrics, alerting and tracing backends

### For the developer persona
* View application and function logs, metrics and request traces
* Provide default out of the box logs, metrics, alerts, traces and dashboards for applications and functions
* Allow creating application and function specific alerting rules to get notified for application and function health issues
* Ability to create shared dashboards for logs and metrics for applications and functions

## Out of scope for 2018
* Multi-tenanted log, metrics and trace collection and access:
    * Per namespace access control
    * Per namespace ingestion quotas
    * Per namespace query quotas
    * Ingestion and query throttling to keep bursty behavior under control
* Supporting log, metric and tracing backends other than the pre-installed ones
* Exporting logs, metrics and traces to different type of backends outside the cluster
* Customized log parsing and normalization per namespace

## Execution - Phase 1
In this phase, we will enable a shared infrastructure where everyone has access to all data. No personas specific experience or access will be provided.

The following components & services will be enabled in this phase:
* Prometheus
* Prometheus Operator
* Grafana
* ElasticSearch
* Kibana
* Zipkin
* Fluentd

Logs from the following locations will be collected:
* stderr & stdout for all application and function containers
* Build logs

Following metrics will be collected:
* Envoy, Istio Mixer (per request metrics), Istio Pilot
* Node metrics (CPU, Memory & Disk)
* Elafros metrics

## Execution - Phase 2
In this phase, we will define and implement features for the developer persona.
* Define and implement contracts for application and function developers for logging, metrics, alerting and distributed tracing
* Write step-by-step guidelines for developers to debug issues throughout the lifecycle of their applications and functions
* Provide developer samples written in Golang. Support for other languages will come in a later phase

## Execution - Phase 3
In this phase, we will define and implement features for the operator persona.
* Define and implement contracts for cluster operators for logging, metrics and alerting
* Define and implement contracts for cluster operators for fine tuning the scale, performance and features of the backends
* Implement auto-scaling of logging, metrics, alerting and tracing backends
* Write step-by-step guidelines for operators to debug issues in the cluster

## Execution - Phase 4
* Provide developer samples written in Node.js, Java, Python, PHP, .Net and Ruby
