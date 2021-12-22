//go:build upgrade
// +build upgrade

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

package upgrade

import (
	"testing"

	"go.uber.org/zap"
	_ "knative.dev/pkg/system/testing"
	pkgupgrade "knative.dev/pkg/test/upgrade"
	"knative.dev/serving/test/upgrade/installation"
)

func TestServingUpgrades(t *testing.T) {
	c := newUpgradeConfig(t)
	suite := pkgupgrade.Suite{
		Tests: pkgupgrade.Tests{
			PreUpgrade:    ServingPreUpgradeTests(),
			PostUpgrade:   ServingPostUpgradeTests(),
			PostDowngrade: ServingPostDowngradeTests(),
			Continual:     ContinualTests(),
		},
		Installations: pkgupgrade.Installations{
			Base: []pkgupgrade.Operation{
				CreateTestNamespace(),
				// The initial Knative Serving version is already installed by scripts.
			},
			UpgradeWith: []pkgupgrade.Operation{
				installation.Head(),
			},
			DowngradeWith: []pkgupgrade.Operation{
				installation.LatestRelease(),
			},
		},
	}
	suite.Execute(c)
}

func newUpgradeConfig(t *testing.T) pkgupgrade.Configuration {
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{"stdout"}
	return pkgupgrade.Configuration{T: t, LogConfig: pkgupgrade.LogConfig{Config: config}}
}
