# Serving Webhook based API Coverage Image

This directory contains the HTTP Server image used in Webhook based API
coverage tool. Core infra pieces for the tool comes from [test-infra](
https://github.com/knative/test-infra/tree/master/tools/webhook-apicoverage).
Knative serving specific pieces of the tool (such as rules, ignored fields)
resides in this directory:

1. [Coverage Rules](coverage_rules.go): contains all the apicoverage rules
   specified for knative serving.
1. [Display Rules](display_rules.go): contains all the display rules
   used by knative serving to display json type like result.
1. [Webhook Handlers](webhook_handlers.go): containes all the handlers that
   the HTTP server exposes.
1. [Ignored Fields](ignoredfields.yaml): Yaml file specifies all the fields
   that are ignored for API Coverage calculation.
1. [Webhook Server](webhook_server.go): contains the HTTP server used by
   the webhhok.