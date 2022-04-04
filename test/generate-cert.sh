#!/usr/bin/env bash

# Copyright 2022 The Knative Authors
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

SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-knative-serving}"
TEST_NAMESPACE=serving-tests
out_dir="$(mktemp -d /tmp/certs-XXX)"
san="knative"

# Generate Root key and cert.
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -subj '/O=Example/CN=Example' -keyout "${out_dir}"/root.key -out "${out_dir}"/root.crt

# Create server key
openssl req -out "${out_dir}"/tls.csr -newkey rsa:2048 -nodes -keyout "${out_dir}"/tls.key -subj "/CN=Example/O=Example" -addext "subjectAltName = DNS:$san"

# Create server certs
openssl x509 -req -extfile <(printf "subjectAltName=DNS:$san") -days 365 -in "${out_dir}"/tls.csr -CA "${out_dir}"/root.crt -CAkey "${out_dir}"/root.key -CAcreateserial -out "${out_dir}"/tls.crt

# Create secret
kubectl create -n ${SYSTEM_NAMESPACE} secret generic serving-ca \
    --from-file=ca.crt="${out_dir}"/root.crt --dry-run=client -o yaml | kubectl apply -f -

kubectl create -n ${SYSTEM_NAMESPACE} secret tls server-certs \
    --key="${out_dir}"/tls.key \
    --cert="${out_dir}"/tls.crt --dry-run=client -o yaml | kubectl apply -f -
