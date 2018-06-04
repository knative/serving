# API Objects - Service

* Orchestrates Routes and Configurations to support higher-level tooling.
  * Configuration of code + inbound routing in a single API call.
  * Definition of specific common rollout policies
    * Automatic (latest) rollout
    * Manual rollout
    * Extensible for other rollout strategies which might be requested
  * Provides a top-level resource for graphical UI to key off of.
* Recommended starting point for Knative Serving use, as it provides "guide rails" to avoid unusual and non-idiomatic usage.
  * Mirrors labels and annotations to the created objects.
  * Advanced scenarios may require disconnecting the Service ownership of the created Route and Configuration.
