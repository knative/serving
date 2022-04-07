package networking

import (
	"context"
	"fmt"
	"strings"

	networkingpkg "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/logging"
)

// GetHTTPOption get http-protocol from resource annotations if not, get it from configmap config-network
func GetHTTPOption(ctx context.Context, networkConfig *networkingpkg.Config, annotations map[string]string) (netv1alpha1.HTTPOption, error) {
	// Get HTTPOption via annotations.
	if len(annotations) != 0 && networking.GetHTTPProtocol(annotations) != "" {
		protocol := strings.ToLower(networking.GetHTTPProtocol(annotations))
		switch networkingpkg.HTTPProtocol(protocol) {
		case networkingpkg.HTTPEnabled:
			return netv1alpha1.HTTPOptionEnabled, nil
		case networkingpkg.HTTPRedirected:
			return netv1alpha1.HTTPOptionRedirected, nil
		default:
			return "", fmt.Errorf("incorrect http-protocol annotation: " + protocol)
		}
	}

	// Get logger from context
	logger := logging.FromContext(ctx)

	// Get HTTPOption via config-network.
	switch httpProtocol := networkConfig.HTTPProtocol; httpProtocol {
	case networkingpkg.HTTPEnabled:
		return netv1alpha1.HTTPOptionEnabled, nil
	case networkingpkg.HTTPRedirected:
		return netv1alpha1.HTTPOptionRedirected, nil
	// This will be deprecated soon
	case networkingpkg.HTTPDisabled:
		logger.Warnf("http-protocol %s in config-network ConfigMap will be deprecated soon", httpProtocol)
		return "", nil
	default:
		logger.Warnf("http-protocol %s in config-network ConfigMap is not supported", httpProtocol)
		return "", nil
	}
}
