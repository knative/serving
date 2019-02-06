/*
Copyright 2019 The Knative Authors
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

package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/knative/serving/test/types"
)

func headers(r *http.Request) map[string]string {
	headerMap := map[string]string{}
	for name, headers := range r.Header {
		name = strings.ToLower(name)
		for i, h := range headers {
			if len(headers) > 1 {
				headerMap[fmt.Sprintf("%s[%d]", name, i)] = h
			} else {
				headerMap[fmt.Sprintf("%s", name)] = h
			}
		}
	}
	return headerMap
}

func requestInfo(r *http.Request) *types.RequestInfo {
	return &types.RequestInfo{
		Ts:      time.Now(),
		URI:     r.RequestURI,
		Host:    r.Host,
		Method:  r.Method,
		Headers: headers(r),
	}
}
