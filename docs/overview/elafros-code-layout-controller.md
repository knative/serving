# Code layout - Controller

* `./pkg/controller/{configuration,revision,route,service}/controller.go`
* `sync_handler` is the main method for reconcile
* Other resource creation broken into their own files in those directories
* Communicates information via `[resource].status` field
