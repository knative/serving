#!/bin/bash

# Copyright 2018 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Modifies /etc/hosts based on ingress rules. Once top-level domain
# provisioning is set up, this should no longer be necessary.

INGRESS_IP=$(kubectl get svc --namespace=istio-system istio-ingress -o template --template='{{ range .status.loadBalancer.ingress }} {{ .ip }} {{ end }}' | tr -d '[:space:]')

INGRESSES=$(kubectl get ingress --namespace=default-app -o template --template='{{ range .items }}{{ range .spec.rules }}{{ .host }}{{ "\n" }}{{ end }}{{ end }}')

echo "Ingress IP is: $INGRESS_IP"
echo "Ingresses are: $INGRESSES"

sudo sed -i '/# BEGINAUTOBLOCK/,/# ENDAUTOBLOCK/d' /etc/hosts

echo "Adding ingresses to /etc/hosts..."
echo "# BEGINAUTOBLOCK" | sudo tee -a /etc/hosts
while read -r ingress; do
  echo "$INGRESS_IP $ingress" | sudo tee -a /etc/hosts
done <<< "$INGRESSES"
echo "# ENDAUTOBLOCK" | sudo tee -a /etc/hosts


