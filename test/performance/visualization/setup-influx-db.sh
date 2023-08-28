#!/usr/bin/env bash

# Copyright 2023 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


# This script configures an influx-db to the state that is expected by the tests
# To run this, make sure you have set env:
#   INFLUX_URL
#   INFLUX_TOKEN

set -o errexit
set -o nounset
set -o pipefail

declare INFLUX_URL
declare INFLUX_TOKEN

orgname=Knativetest
bucketname=knative-serving

if [[ -z "${INFLUX_URL}" ]]; then
  echo "env variable 'INFLUX_URL' not specified!"
  exit 1
fi
if [[ -z "${INFLUX_TOKEN}" ]]; then
  echo "env variable 'INFLUX_TOKEN' not specified!"
  exit 1
fi

echo "# Checking login"
curl -X GET "$INFLUX_URL/api/v2" -H "Authorization: Token $INFLUX_TOKEN"

echo "# Setting up organization and bucket"
status_code=$(curl -s --output /dev/null --write-out %{http_code} -X GET "$INFLUX_URL/api/v2/orgs?org=$orgname" -H "Authorization: Token $INFLUX_TOKEN")

if [[ "$status_code" -ne 404 ]] ; then
  echo "> Organization already exists"
else
  echo "> Creating organization $orgname"
  curl -X POST "$INFLUX_URL/api/v2/orgs" -H "Authorization: Token $INFLUX_TOKEN" -H "Content-Type: application/json" \
     --data "
       {
         \"name\": \"$orgname\",
         \"description\": \"Knative Serving Performance tests\"
       }"
fi

status_code=$(curl -s --output /dev/null --write-out %{http_code} -X GET "$INFLUX_URL/api/v2/buckets?org=$orgname&name=$bucketname" -H "Authorization: Token $INFLUX_TOKEN")

if [[ "$status_code" -ne 404 ]] ; then
  echo "> Bucket already exists"
else
  echo "> Creating bucket $bucketname"
  orgID=$(curl -X GET "$INFLUX_URL/api/v2/orgs?org=$orgname" -H "Authorization: Token $INFLUX_TOKEN" | jq -r '.orgs[0].id')
  curl -X POST "$INFLUX_URL/api/v2/buckets" -H "Authorization: Token $INFLUX_TOKEN" -H "Content-Type: application/json" \
     --data "
       {
         \"name\": \"$bucketname\",
         \"orgID\": \"$orgID\",
         \"description\": \"Knative Serving Performance tests\"
       }"
fi

