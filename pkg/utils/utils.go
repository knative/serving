/*
Copyright 2018 The Knative Authors

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

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	resolverFileName = "/etc/resolv.conf"
)

// GetClusterDomainName returns cluster's domain name or error
func GetClusterDomainName() (string, error) {
	if _, err := os.Stat(resolverFileName); err != nil {
		return "", err
	}
	f, err := os.Open(resolverFileName)
	if err != nil {
		return "", err
	}
	defer f.Close()

	return getClusterDomainName(f)
}

func getClusterDomainName(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		elements := strings.Split(scanner.Text(), " ")
		if elements[0] == "search" {
			for i := 1; i < len(elements)-1; i++ {
				if strings.HasPrefix(elements[i], "svc.") {
					return strings.Split(elements[i], "svc.")[1], nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s does not seem to be a valid kubernetes resolv.conf file", resolverFileName)
}
