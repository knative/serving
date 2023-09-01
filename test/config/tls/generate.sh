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

# This script generates test/config/tls/cert-secret.yaml.

san="knative.dev"

# Create CA key and cert
openssl req -x509 -sha256 -nodes -days 3650 -newkey rsa:2048 -subj '/O=Knative Community/CN=example.com' -keyout rootCAKey.pem -out rootCACert.pem

# Create server key
openssl req -out tls.csr -newkey rsa:2048 -nodes -keyout tls.key -subj "/CN=example.com/O=Knative Community" -addext "subjectAltName = DNS:$san"

# Create server certs
openssl x509 -req -extfile <(printf "subjectAltName=DNS:$san") -days 3650 -in tls.csr -CA rootCACert.pem -CAkey rootCAKey.pem -CAcreateserial -out tls.crt

CA_CERT=$(cat rootCACert.pem | base64  | tr -d '\n')
TLS_KEY=$(cat tls.key | base64  | tr -d '\n')
TLS_CERT=$(cat tls.crt | base64  | tr -d '\n')

cat <<EOF > cert-secret.yaml
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

apiVersion: v1
kind: Secret
metadata:
  name: ca-cert
  namespace: serving-tests
data:
  ca.crt: ${CA_CERT}
---
apiVersion: v1
kind: Secret
metadata:
  name: server-certs
  namespace: knative-serving
data:
  tls.key: ${TLS_KEY}
  tls.crt: ${TLS_CERT}
EOF

# Clean up
rm -f rootCACert.pem rootCAKey.pem tls.key tls.crt tls.csr rootCACert.srl
