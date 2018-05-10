# Motivation

The goal of the Elafros project is to provide a common toolkit and API
framework for serverless workloads.

We define serverless workloads as computing workloads that are:

* Stateless
* Amenable to the process scale-out model
* Primarily driven by application level (L7 -- HTTP, for example)
  request traffic

While Kubernetes provides basic primitives like Deployment, Service,
and Ingress in support of this model, our experience suggests that a
more compact and richer opinionated model has substantial benefit for
developers. In particular, by standardizing on higher-level primitives
which perform substantial amounts of automation of common
infrastructure, it should be possible to build consistent toolkits
that provide a richer experience than updating yaml files with
`kubectl`.

The Elafros APIs consist of Compute API (these documents),
[Build API](https://github.com/elafros/build) and
[Eventing API](https://github.com/elafros/eventing).
