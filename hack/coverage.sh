#!/bin/bash
go test ../pkg/... -coverprofile coverage_profile.txt
gsutil cp coverage_profile.txt gs://gke-prow/pr-logs/directory/elafros-coverage/
