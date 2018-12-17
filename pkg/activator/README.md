# About Activator

The name *activator* is actually a misnomer, since after Knative 0.2,
*activator* no longer activates inactive Revisions.  The only functionalities of
the activator are:

* Receiving & buffering requests for inactive Revisions.
* Reporting metrics to the autoscaler.
* Retrying requests to a Revision after the autoscaler scales such Revision
  based on the reported metrics.
