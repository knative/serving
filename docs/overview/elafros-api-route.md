# API Objects - Route

* Provides a (automatic) DNS name based on cluster-level prefix
* Top level resource traffic assignment and naming
  * Points to Revision's k8s service
  * Can specify latest from Configuration, will follow redirect
* Allows for various rollout strategies
  * Auto (point to Configuration), will follow latest Ready revision stamped from it
  * Manual (point to Revision directly), traffic pinned
  * Combinations of the above
* Creates Istio + k8 resources
  * Creates a “placeholder” k8s service for when there’s no backing revisions
  * Creates Istio ingress
  * Traffic routing done via Istio route_rules
