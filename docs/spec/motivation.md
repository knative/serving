The goal of the Elafros project is to provide a common toolkit and API
framework for serverless workloads.

We define serverless workloads as computing workloads which are:

* Stateless.
* Amenable to the process scale-out model.
* Primarily driven by application level (L7 -- HTTP, for example)
  request traffic.

While Kubernetes provides basic primitives like Deployment, Service,
and Ingress in support of this model, our experience suggests that a
more compact and richer opinionated model has substantial benefit for
developers. In particular, by standardizing on higher-level primitives
which perform substantial amounts of automation of common primitives,
it should be possible to build consistent toolkits which provide a
richer experience than via yaml files with `kubectl`.

The Elafos APIs are composed of three facets; this document primarily
concerns itself with the compute API; the other two APIs are Build and
Eventing.
