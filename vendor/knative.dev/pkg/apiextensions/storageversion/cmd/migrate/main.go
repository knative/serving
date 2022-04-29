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

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"go.uber.org/zap"
	apixclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/apiextensions/storageversion"
	"knative.dev/pkg/environment"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

const (
	IgnoreNotFoundEnvKey = "IGNORE_NOT_FOUND"
)

func main() {
	logger := setupLogger()
	defer logger.Sync()

	env := environment.ClientConfig{}
	env.InitFlags(flag.CommandLine)

	flag.Parse()

	config, err := env.GetRESTConfig()
	if err != nil {
		logger.Fatalf("failed to get kubeconfig %s", err)
	}

	grs, err := parseResources(flag.Args())
	if err != nil {
		logger.Fatal(err)
	}

	ignoreNotFound, _ := strconv.ParseBool(os.Getenv(IgnoreNotFoundEnvKey))

	migrator := storageversion.NewMigrator(
		dynamic.NewForConfigOrDie(config),
		apixclient.NewForConfigOrDie(config),
	)

	ctx := signals.NewContext()

	logger.Infof("Migrating %d group resources", len(grs))

	for _, gr := range grs {
		logger.Info("Migrating group resource ", gr)
		if err := migrator.Migrate(ctx, gr); err != nil {
			if ignoreNotFound && apierrs.IsNotFound(err) {
				logger.Infof("Ignoring resource migration - unable to fetch crd %s - %s", gr, err)
				continue
			}
			logger.Fatal("Failed to migrate: ", err)
		}
	}

	logger.Info("Migration complete")
}

func parseResources(args []string) ([]schema.GroupResource, error) {
	grs := make([]schema.GroupResource, 0, len(args))
	for _, arg := range args {
		gr := schema.ParseGroupResource(arg)
		if gr.Empty() {
			return nil, fmt.Errorf("unable to parse group version: %s", arg)
		}
		grs = append(grs, gr)
	}
	return grs, nil
}

func setupLogger() *zap.SugaredLogger {
	const component = "storage-migrator"

	config, err := logging.NewConfigFromMap(nil)
	if err != nil {
		log.Fatal("Failed to create logging config: ", err)
	}

	logger, _ := logging.NewLoggerFromConfig(config, component)
	return logger
}
