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

echo "Deleting Configurations, Revisions, Routes and WarmImages"

for f in $(kubectl get configurations -o name | cut -d'/' -f 2); do kubectl delete configurations --ignore-not-found=true $f; done
for f in $(kubectl get revisions -o name | cut -d'/' -f 2); do kubectl delete revisions --ignore-not-found=true $f; done
for f in $(kubectl get routes -o name | cut -d'/' -f 2); do kubectl delete routes --ignore-not-found=true $f; done
for f in $(kubectl get warmimages -o name | cut -d'/' -f 2); do kubectl delete warmimages --ignore-not-found=true $f; done

kubectl delete namespace default-ela --ignore-not-found=true
