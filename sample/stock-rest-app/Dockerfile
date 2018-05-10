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
FROM golang

ENV SRC_DIR=/go/src/github.com/elafros/elafros

WORKDIR $SRC_DIR
ADD . $SRC_DIR

RUN go build ./sample/stock-rest-app

ENTRYPOINT $SRC_DIR/stock-rest-app