#!/bin/bash

set -e

grep -r -L "Copyright 2018 Google LLC" $1  \
  | grep -P ".go$" \
  | xargs -I {} sh -c \
  'cat hack/boilerplate/boilerplate.go.txt > /tmp/boilerplate && cat {} >> /tmp/boilerplate && mv /tmp/boilerplate {}'
