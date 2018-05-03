/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package model

import (
        "crypto/rand"
        "encoding/base32"
        "strings"
)

func newId() string {
        bytes := make([]byte, 16)
        rand.Read(bytes)
        str := base32.StdEncoding.EncodeToString(bytes)
        return strings.TrimSuffix(str, "======")
}
