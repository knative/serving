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
	"strings"
	"text/template"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	pkgnet "knative.dev/pkg/network"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"

	"knative.dev/pkg/apis"
)

// HTTPScheme is the string representation of http.
const HTTPScheme string = "http"

// GetAllDomainsAndTags returns all of the domains and tags(including subdomains) associated with a Route
func GetAllDomainsAndTags(ctx context.Context, r *v1alpha1.Route, names []string, localServiceNames sets.String) (map[string]string, error) {
	domainTagMap := make(map[string]string)

	for _, name := range names {
		meta := r.ObjectMeta.DeepCopy()

		hostname, err := HostnameFromTemplate(ctx, meta.Name, name)
		if err != nil {
			return nil, err
		}

		labels.SetVisibility(meta, localServiceNames.Has(hostname))

		subDomain, err := DomainNameFromTemplate(ctx, *meta, hostname)
		if err != nil {
			return nil, err
		}
		domainTagMap[subDomain] = name
	}
	return domainTagMap, nil
}

// DomainNameFromTemplate generates domain name base on the template specified in the `config-network` ConfigMap.
// name is the "subdomain" which will be referred as the "name" in the template
func DomainNameFromTemplate(ctx context.Context, r v1.ObjectMeta, name string) (string, error) {
	domainConfig := config.FromContext(ctx).Domain
	rLabels := r.Labels
	domain := domainConfig.LookupDomainForLabels(rLabels)
	annotations := r.Annotations
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

	var templ *template.Template
	// If the route is "cluster local" then don't use the user-defined
	// domain template, use the default one
	if rLabels[config.VisibilityLabelKey] == config.VisibilityClusterLocal {
		templ = template.Must(template.New("domain-template").Parse(
			network.DefaultDomainTemplate))
	} else {
		templ = networkConfig.GetDomainTemplate()
	}

	if err := templ.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("error executing the DomainTemplate: %w", err)
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
		return "", fmt.Errorf("error executing the TagTemplate: %w", err)
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

// IsClusterLocal checks if a domain is only visible with cluster.
func IsClusterLocal(domain string) bool {
	return strings.HasSuffix(domain, pkgnet.GetClusterDomainName())
}
