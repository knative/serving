#!/usr/bin/env bash

function resolve_resources(){
  local dir=$1
  local resolved_file_name=$2
  local version=$3

  echo "Writing resolved yaml to $resolved_file_name"

  > "$resolved_file_name"

  for yaml in `find $dir -name "*.yaml" | sort`; do
    resolve_file "$yaml" "$resolved_file_name" "$version"
  done
}

function resolve_file() {
  local file=$1
  local to=$2
  local version=$3

  echo "---" >> "$to"
  # 1. Rewrite image references
  # 2. Update config map entry
  # 3. Replace serving.knative.dev/release label.
  # 4. Remove seccompProfile, except on CRD files in 300-resources folder to avoid breaking CRDs.
  if [[ $file == */300-resources/* ]]; then
    sed -e "s+app.kubernetes.io/version: devel+app.kubernetes.io/version: \"""$version""\"+" \
        -e "s+type: RuntimeDefault++" \
        "$file" >> "$to"
  else
    sed -e "s+app.kubernetes.io/version: devel+app.kubernetes.io/version: \"""$version""\"+" \
        -e "s+seccompProfile:++" \
        -e "s+type: RuntimeDefault++" \
        "$file" >> "$to"
  fi
}
