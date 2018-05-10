#!/bin/bash
#
# Add boilerplate.go.txt to all .go files missing it in a directory.
#
# Usage: (from repository root)
#        ./hack/boilerplate/add-boilerplate-go.sh <DIR>

set -e

grep -r -L -P "Copyright \d+ Google" $1  \
  | grep -P ".go$" \
  | xargs -I {} sh -c \
  'cat hack/boilerplate/boilerplate.go.txt {} > /tmp/boilerplate && mv /tmp/boilerplate {}'
