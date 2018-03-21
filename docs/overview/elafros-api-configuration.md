# API Objects - Configuration

* Users and tools mainly interact with this resource
  * "Deploy new code"
  * "Update environment"
  * "Change backend"
* On modifications, new Revision is created
* Handles build creation and creates linear revision history
* Also records "latestCreated" and "latestReady" Revisions, for use by tooling

## TODO:

* Expand on the above bullets
