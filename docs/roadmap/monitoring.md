# 2018 Roadmap for Monitoring and Logging

This document captures what we hope to accomplish in 2018 in Monitoring and Logging areas for Elafros. 

## Overview
We will provide to distinct experiences, one for the [operator persona](../product/personas.md#operator-personas) and one for the [developer persona](../product/personas.md#developer-personas).

### Operator Capabilities
* Provide default collection of cluster logs and metrics from infrastructure components such as Kubernetes.
* Provide default dashboards and interfaces for viewing cluster logs and metrics.
* Allow setting custom alerts on cluster events.
* Auto-scale the default logging, metrics, alerting and tracing backends.
* Allow fine tuning of scale, performance and features of the default logging, metrics, alerting and tracing backends.

### Developer Capabilities
* Provide default collection of logs, metrics, and request traces.
* Provide default dashboards and interfaces for viewing logs, metrics and traces, and for setting alerts on the same.
* Allow setting custom application and function alerts.
* Allow creating shared dashboards for logs and metrics for applications and functions.

## M3 Deliverables (Due by March 31, 2018) - Basics
In this phase, we will enable a shared infrastructure where everyone has access to all data. No personas specific experience or access will be provided.

The following items will be installed and secured in a cluster by default, but we will provide the ability to replace or remove these in a later milestone.
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
* Node and pod level metrics (CPU, memory, disk and network)
* Elafros controller metrics

Request logs from Istio proxy, user applications and user functions will be collected by Zipkin.

## M4 Deliverables (Due by April 30, 2018) - Developer Contracts
In this phase, we will define and implement features for the developer persona.
* Define and implement developer contracts for logging, metrics, alerting and tracing.
* Write step-by-step guidelines for developers to debug issues throughout the lifecycle of their applications and functions.
* Provide developer samples written in Golang. Support for other languages will come in a later phase.

## M5 Deliverables (Due by May 31, 2018) - Operator Contracts
In this phase, we will define and implement features for the operator persona.
* Define and implement operator contracts for customizing default logging, metrics and alerting backends.
* Deploy operator specific instances of the default backends to separate access of operators vs developers.
* Write step-by-step guidelines for operators to debug issues in the cluster.

## M6 Deliverables (Due by June 30, 2018) - Operator Contracts Continued
* Define and implement operator contracts for plugging in custom logging, metrics, alerting and tracing backends. We will not provide maintenance, rollout processes, etc for third-party monitoring, logging, or tracing extensions, though we may maintain a "contrib" directory for such contributions.

* Add support for one managed solution like StackDriver.

## M7 and Onwards
* Allow namespace specific instances of default backends for namespace level access control.
* Implement auto-scaling of the default backends.
* Implement upgrading of the default backends.
* Implement maintenance the default backends such as data retention.
* Provide developer samples written in Node.js, Java, Python, PHP, .Net and Ruby.

## Out of Scope for 2018
* Improving the underlying logging, monitoring, and tracing systems to support multi-tenancy.
