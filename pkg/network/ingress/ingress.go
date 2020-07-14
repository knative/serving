/*
Copyright 2019 The Knative Authors.

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

package ingress

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	net "knative.dev/serving/pkg/network"
)

// ComputeHash computes a hash of the Ingress Spec, Namespace and Name
func ComputeHash(ing *v1alpha1.Ingress) ([sha256.Size]byte, error) {
	bytes, err := json.Marshal(ing.Spec)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("failed to serialize Ingress: %w", err)
	}
	bytes = append(bytes, []byte(ing.GetNamespace())...)
	bytes = append(bytes, []byte(ing.GetName())...)
	return sha256.Sum256(bytes), nil
}

// InsertProbe adds a AppendHeader rule so that any request going through a Gateway is tagged with
// the version of the Ingress currently deployed on the Gateway.
// TODO: move this to github.com/knative/networking — currently it is used by downstream
// consumers, see: https://github.com/knative/serving/issues/7482.
func InsertProbe(ing *v1alpha1.Ingress) (string, error) {
	bytes, err := ComputeHash(ing)
	if err != nil {
		return "", fmt.Errorf("failed to compute the hash of the Ingress: %w", err)
	}
	hash := fmt.Sprintf("%x", bytes)

	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			return "", fmt.Errorf("rule is missing HTTP block: %+v", rule)
		}
		for i := range rule.HTTP.Paths {
			if rule.HTTP.Paths[i].AppendHeaders == nil {
				rule.HTTP.Paths[i].AppendHeaders = make(map[string]string, 1)
			}
			rule.HTTP.Paths[i].AppendHeaders[net.HashHeaderName] = hash
		}
	}

	return hash, nil
}

// HostsPerVisibility takes an Ingress and a map from visibility levels to a set of string keys,
// it then returns a map from that key space to the hosts under that visibility.
func HostsPerVisibility(ing *v1alpha1.Ingress, visibilityToKey map[v1alpha1.IngressVisibility]sets.String) map[string]sets.String {
	output := make(map[string]sets.String, 2) // We currently have public and internal.
	for _, rule := range ing.Spec.Rules {
		for host := range ExpandedHosts(sets.NewString(rule.Hosts...)) {
			for key := range visibilityToKey[rule.Visibility] {
				if _, ok := output[key]; !ok {
					output[key] = make(sets.String, len(rule.Hosts))
				}
				output[key].Insert(host)
			}
		}
	}
	return output
}

// ExpandedHosts sets up hosts for the short-names for cluster DNS names.
func ExpandedHosts(hosts sets.String) sets.String {
	allowedSuffixes := []string{
		"",
		"." + network.GetClusterDomainName(),
		".svc." + network.GetClusterDomainName(),
	}
	// Optimistically pre-alloc.
	expanded := make(sets.String, len(hosts)*len(allowedSuffixes))
	for _, h := range hosts.List() {
		for _, suffix := range allowedSuffixes {
			if strings.HasSuffix(h, suffix) {
				expanded.Insert(strings.TrimSuffix(h, suffix))
			}
		}
	}
	return expanded
}
