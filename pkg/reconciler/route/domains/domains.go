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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
)

// HTTPScheme is the string representation of http.
const HTTPScheme string = "http"

// GetAllDomainsAndTags returns all of the domains and tags(including subdomains) associated with a Route
func GetAllDomainsAndTags(ctx context.Context, r *v1alpha1.Route, names []string) (map[string]string, error) {
	domainTagMap := make(map[string]string)

	for _, name := range names {
		hostname, err := HostnameFromTemplate(ctx, r.Name, name)
		if err != nil {
			return nil, err
		}
		subDomain, err := DomainNameFromTemplate(ctx, r, hostname)
		if err != nil {
			return nil, err
		}
		domainTagMap[subDomain] = name
	}
	return domainTagMap, nil
}

// DomainNameFromTemplate generates domain name base on the template specified in the `config-network` ConfigMap.
// name is the "subdomain" which will be referred as the "name" in the template
func DomainNameFromTemplate(ctx context.Context, r *v1alpha1.Route, name string) (string, error) {
	domainConfig := config.FromContext(ctx).Domain
	domain := domainConfig.LookupDomainForLabels(r.ObjectMeta.Labels)
	annotations := r.ObjectMeta.Annotations
	// These are the available properties they can choose from.
	// We could add more over time - e.g. RevisionName if we thought that
	// might be of interest to people.
	data := network.DomainTemplateValues{
		Name:        name,
		Namespace:   r.Namespace,
		Domain:      domain,
		Annotations: annotations,
	}

	networkConfig := config.FromContext(ctx).Network
	buf := bytes.Buffer{}
	if err := networkConfig.GetDomainTemplate().Execute(&buf, data); err != nil {
		return "", fmt.Errorf("error executing the DomainTemplate: %v", err)
	}
	return buf.String(), nil
}

// HostnameFromTemplate generates domain name base on the template specified in the `config-network` ConfigMap.
// name is the "subdomain" which will be referred as the "name" in the template
func HostnameFromTemplate(ctx context.Context, name string, tag string) (string, error) {
	if tag == "" {
		return name, nil
	}
	// These are the available properties they can choose from.
	// We could add more over time - e.g. RevisionName if we thought that
	// might be of interest to people.
	data := network.TagTemplateValues{
		Name: name,
		Tag:  tag,
	}

	networkConfig := config.FromContext(ctx).Network
	buf := bytes.Buffer{}
	if err := networkConfig.GetTagTemplate().Execute(&buf, data); err != nil {
		return "", fmt.Errorf("error executing the TagTemplate: %v", err)
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
