#!/usr/bin/env bash

set -e

source "$(dirname "${BASH_SOURCE[0]}")/library.sh"

if (( ! IS_PROW )); then
  echo "Local run of shellcheck-presubmit detected"
  echo "This notably DOES NOT ACT LIKE THE GITHUB PRESUBMIT"
  echo "The Github presubmit job only runs shellcheck on files you touch"
  echo "There's no way to locally determine which files you touched:"
  echo " as git is a distributed VCS, there is no notion of parent until merge"
  echo " is attempted."
  echo "So it checks the current content of all files changed in the previous commit"
  echo " and/or currently staged."
fi

shellcheck_new_files
