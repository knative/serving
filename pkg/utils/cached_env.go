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

package utils

import "os"

var (
	cachedEnvValues map[string]string
)

func init() {
	cachedEnvValues = parseEnvStrs(os.Environ())
}

// GetCachedEnv returns the environment variables from a cache
// that is populated during init.
// This method is useful for cases where environment variables are accessed
// frequently. The main use case so far is when writing request logs,
// we access ServerIP from the environment variables for every incoming request.
// os.Getenv does string parsing everytime it is called and calling it as frequenly
// as per request gets expensive.
func GetCachedEnv(key string) string {
	return cachedEnvValues[key]
}

func parseEnvStrs(strs []string) map[string]string {
	m := make(map[string]string)
	for _, s := range strs {
		k, v := parseKeyValue(s)
		if k != "" {
			m[k] = v
		}
	}

	return m
}

func parseKeyValue(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
