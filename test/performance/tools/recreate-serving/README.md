# Re-create Serving Components

This directory contains all the files needed to re-create all the benchmarking clusters. It contains of

* recreate.yaml: Cron job that runs once a day. This will run the script recreate.sh
* recreate.sh: This will:
  * Read all the current clusters in the `knative-performance` project
  * Kill all the current K8S objects
  * Delete the existing cluster
  * Recreate the cluster with the same name, node-count and in the same zone
  * Install knative serving at HEAD
  * Apply patches to setup performance testing
  * Run `ko apply` to all objects in the test-dir. Note that, this assuumes that the dir name and cluster name are the same.
* recreate-test.yaml: K8S Job to make sure `recreate.sh` works as expected
