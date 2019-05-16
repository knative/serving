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

package domains

import (
	"bytes"
	"context"
	"fmt"

	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
)

// HTTPScheme is the string representation of http.
const HTTPScheme string = "http"

// GetAllDomainsAndTags returns all of the domains and tags(including subdomains) associated with a Route
func GetAllDomainsAndTags(ctx context.Context, r *v1alpha1.Route, names []string) ([]string, []string, error) {
	logger := logging.FromContext(ctx)

	// majorDomain, err := DomainNameFromTemplate(ctx, r, r.Name)
	// if err != nil {
	// 	return nil, nil, err
	// }

	// TODO: is major domain tag-less?
	// I assume sets has no order?
	// allDomains := sets.NewString(majorDomain)
	var allDomains, allTags []string
	// allDomains = append(allDomains, majorDomain)
	// allTags = append(allTags, "")
	// logger.Info("major domain: ", majorDomain)
	// logger.Info("dns names: ", names)
	for _, name := range names {
		subDomain, err := DomainNameFromTemplate(ctx, r, SubdomainName(r, name))
		if err != nil {
			return nil, nil, err
		}
		allDomains = append(allDomains, subDomain)
		allTags = append(allTags, name)
		logger.Info("tag: ", name, " subdomain: ", subDomain)
	}
	return allDomains, allTags, nil
}

// SubdomainName generates a name which represents the subdomain of a route
func SubdomainName(r *v1alpha1.Route, suffix string) string {
	if suffix == "" {
		return r.Name
	}
	return fmt.Sprintf("%s-%s", r.Name, suffix)
}

// DomainNameFromTemplate generates domain name base on the template specified in the `config-network` ConfigMap.
// name is the "subdomain" which will be referred as the "name" in the template
func DomainNameFromTemplate(ctx context.Context, r *v1alpha1.Route, name string) (string, error) {
	domainConfig := config.FromContext(ctx).Domain
	domain := domainConfig.LookupDomainForLabels(r.ObjectMeta.Labels)

	// These are the available properties they can choose from.
	// We could add more over time - e.g. RevisionName if we thought that
	// might be of interest to people.
	data := network.DomainTemplateValues{
		Name:      name,
		Namespace: r.Namespace,
		Domain:    domain,
	}

	networkConfig := config.FromContext(ctx).Network
	buf := bytes.Buffer{}
	if err := networkConfig.GetDomainTemplate().Execute(&buf, data); err != nil {
		return "", fmt.Errorf("error executing the DomainTemplate: %v", err)
	}
	return buf.String(), nil
}

// URL generates the a string representation of a URL.
func URL(scheme, fqdn string) *apis.URL {
	return &apis.URL{
		Scheme: scheme,
		Host:   fqdn,
	}
}
