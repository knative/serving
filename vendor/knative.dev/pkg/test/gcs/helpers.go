/*
Copyright 2020 The Knative Authors

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

package gcs

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// get the bucket and object from the gsURL
func linkToBucketAndObject(gsURL string) (string, string, error) {
	gsURL = strings.Replace(gsURL, "gs://", "", 1)

	sIdx := strings.IndexByte(gsURL, '/')
	if sIdx == -1 || sIdx+1 >= len(gsURL) {
		return "", "", fmt.Errorf("the gsUrl (%q) cannot be converted to bucket/object", gsURL)
	}

	return gsURL[:sIdx], gsURL[sIdx+1:], nil
}

// BuildLogPath returns the build log path from the test result gcsURL
func BuildLogPath(gcsURL string) (string, error) {
	u, err := url.Parse(gcsURL)
	if err != nil {
		return gcsURL, err
	}
	u.Path = path.Join(u.Path, "build-log.txt")
	return u.String(), nil
}

// GetConsoleURL returns the gcs link renderable directly from a browser
func GetConsoleURL(gcsURL string) (string, error) {
	u, err := url.Parse(gcsURL)
	if err != nil {
		return gcsURL, err
	}
	u.Path = path.Join("storage/browser", u.Host, u.Path)
	u.Scheme = "https"
	u.Host = "console.cloud.google.com"
	return u.String(), nil
}
