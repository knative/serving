package upgrade

import pkgupgrade "knative.dev/pkg/test/upgrade"

func ContinualTests() []pkgupgrade.BackgroundOperation {
	return []pkgupgrade.BackgroundOperation{
		ProbeTest(),
		AutoscaleSustainingTest(),
		AutoscaleSustainingWithTBCTest(),
	}
}
