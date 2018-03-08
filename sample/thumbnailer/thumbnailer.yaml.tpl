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

apiVersion: elafros.dev/v1alpha1
kind: Route
metadata:
  name: thumb
  namespace: default
spec:
  domainSuffix: thumb.googlecustomer.net
  traffic:
  - configurationName: thumbtemplate
    percent: 100
---
apiVersion: elafros.dev/v1alpha1
kind: Configuration
metadata:
  name: thumbtemplate
  namespace: default
spec:
  build:
    source:
      git:
        url: https://github.com/mchmarny/rester-tester
        branch: master
    template:
      name: docker-build-push-helper
      arguments:
      - name: IMAGE
        value: &image DOCKER_REPO_OVERRIDE/rester-tester
  template:
    spec:
      serviceType: container
      containerSpec:
        image: *image
        env:
          - name: TARGET
            value: thumb_v1
