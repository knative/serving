#!/bin/bash
go test ../pkg/... -coverprofile coverage_profile.txt
gsutil cp -a project-private coverage_profile.txt gs://gke-prow/pr-logs/directory/elafros-coverage/$PULL_PULL_SHA
