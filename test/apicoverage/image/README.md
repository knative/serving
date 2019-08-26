# Serving Webhook based API Coverage Image

This directory contains the HTTP Server image used in Webhook based API coverage
tool. Core infra pieces for the tool comes from
[knative.dev/pkg](https://github.com/knative/pkg/tree/master/test/webhook-apicoverage).
Knative serving specific pieces of the tool (such as rules, ignored fields)
resides in this directory.
