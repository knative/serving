# Update Serving Components

This directory contains all the files needed to update the knative serving
master in all the clusters. It contains of

- update.yaml: Cron job that runs once an hour. This will run the script
  `update.sh`
- update-test.yaml: K8S Job that uses the image
  `gcr.io/knative-performance/update-serving:test` to test script works as
  expected
- update.sh: This will read all the current clusters in the
  `knative-performance` project and update knative serving to HEAD
