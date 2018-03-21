# Code layout - Controller

* ./pkg/controller/{route,configuration,revision}/controller.go
* sync_handler the main method for reconcile
* Other resource creation broken into their own files in those directories
* Communicates information via [resource].status field
