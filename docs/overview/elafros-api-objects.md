# API Objects

* Configuration
  * Desired current state of deployment (#HEAD)
  * Records both code and configuration (separated, ala 12 factor)
  * Stamps out revisions as it is updated
* Revision
  * Code and configuration snapshot
  * k8s infra: Deployment, ReplicaSet, Pods, etc
* Route
  * Traffic assignment to Revisions (fractional scaling or by name)
  * Built using Istio
* Build
  * Executes builds
  
<img src="./images/api-objects.png" width="250">


