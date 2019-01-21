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

package resources

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
)

const portNameSeparator = ":"

// GetServers gets the `Servers` from `Gateway` that belongs to the given ClusterIngress.
func GetServers(gateway *v1alpha3.Gateway, ci *v1alpha1.ClusterIngress) []v1alpha3.Server {
	servers := []v1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		if belongsToClusterIngress(&gateway.Spec.Servers[i], ci) {
			servers = append(servers, gateway.Spec.Servers[i])
		}
	}
	return sortServers(servers)
}

func belongsToClusterIngress(server *v1alpha3.Server, ci *v1alpha1.ClusterIngress) bool {
	// The format of the portName should be "<clusteringress-name>:<number>".
	// For example, route-test:0.
	portNameSplits := strings.Split(server.Port.Name, portNameSeparator)
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == ci.Name
}

func sortServers(servers []v1alpha3.Server) []v1alpha3.Server {
	sort.Slice(servers, func(i, j int) bool {
		switch strings.Compare(servers[i].Port.Name, servers[j].Port.Name) {
		case -1:
			return true
		case 1:
			return false
		}
		return true
	})
	return servers
}

// MakeServers creates the expected Gateway `Servers` according to the given
// ClusterIngress.
func MakeServers(ci *v1alpha1.ClusterIngress) []v1alpha3.Server {
	servers := []v1alpha3.Server{}
	// TODO(zhiminx): for the hosts that does not included in the ClusterIngressTLS but listed in the ClusterIngressRule,
	// do we consider them as hosts for HTTP?
	for i := range ci.Spec.TLS {
		tls := ci.Spec.TLS[i]
		servers = append(servers, v1alpha3.Server{
			Hosts: tls.Hosts,
			Port: v1alpha3.Port{
				Name:     fmt.Sprintf("%s%s%d", ci.Name, portNameSeparator, i),
				Number:   443,
				Protocol: "HTTPS",
			},
			TLS: &v1alpha3.TLSOptions{
				Mode:              v1alpha3.TLSModeSimple,
				ServerCertificate: tls.ServerCertificate,
				PrivateKey:        tls.PrivateKey,
				// TODO(zhiminx): When Istio 1.1 is ready, set "credentialName" of TLS
				// with the secret name.
			},
		})
	}
	return sortServers(servers)
}

// UpdateGateway replaces the existing servers with the wanted servers.
func UpdateGateway(gateway *v1alpha3.Gateway, want []v1alpha3.Server, existing []v1alpha3.Server) *v1alpha3.Gateway {
	existingServers := make(map[string]*v1alpha3.Server)
	for i := range existing {
		existingServers[existing[i].Port.Name] = &existing[i]
	}

	servers := []v1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		// We remove
		//  1) the existing servers
		//  2) the default HTTP server and HTTPS server in the gateway because they are only used for the scenario of not reconciling gateway.
		_, ok := existingServers[gateway.Spec.Servers[i].Port.Name]
		if ok || isDefaultServer(&gateway.Spec.Servers[i]) {
			continue
		}
		servers = append(servers, gateway.Spec.Servers[i])
	}
	servers = append(servers, want...)
	sortServers(servers)
	gateway.Spec.Servers = servers
	return gateway
}

func isDefaultServer(server *v1alpha3.Server) bool {
	return server.Port.Name == "http" || server.Port.Name == "https"
}
