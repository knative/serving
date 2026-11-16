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

package storageversion

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"go.uber.org/zap"
	apix "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apixclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/pager"
	"knative.dev/pkg/environment"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

// Migrator will read custom resource definitions and upgrade
// the associated resources to the latest storage version
type Migrator struct {
	dynamicClient dynamic.Interface
	apixClient    apixclient.Interface
}

// NewMigrator will return a new Migrator
func NewMigrator(d dynamic.Interface, a apixclient.Interface) *Migrator {
	return &Migrator{
		dynamicClient: d,
		apixClient:    a,
	}
}

// Migrate takes a group resource (ie. resource.some.group.dev) and
// updates instances of the resource to the latest storage version
//
// This is done by listing all the resources and performing an empty patch
// which triggers a migration on the K8s API server
//
// Finally the migrator will update the CRD's status and drop older storage
// versions
func (m *Migrator) Migrate(ctx context.Context, gr schema.GroupResource) error {
	crdClient := m.apixClient.ApiextensionsV1().CustomResourceDefinitions()

	crd, err := crdClient.Get(ctx, gr.String(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to fetch crd %s - %w", gr, err)
	}

	version := storageVersion(crd)

	if version == "" {
		return fmt.Errorf("unable to determine storage version for %s", gr)
	}

	// don't migrate storage version if CRD has a single valid storage in its status
	if len(crd.Status.StoredVersions) == 1 && crd.Status.StoredVersions[0] == version {
		return nil
	}

	if err := m.migrateResources(ctx, gr.WithVersion(version)); err != nil {
		return err
	}

	patch := `{"status":{"storedVersions":["` + version + `"]}}`
	_, err = crdClient.Patch(ctx, crd.Name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("unable to drop storage version definition %s - %w", gr, err)
	}

	return nil
}

func (m *Migrator) migrateResources(ctx context.Context, gvr schema.GroupVersionResource) error {
	client := m.dynamicClient.Resource(gvr)

	listFunc := func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return client.Namespace(metav1.NamespaceAll).List(ctx, opts)
	}

	onEach := func(obj runtime.Object) error {
		item := obj.(metav1.Object)

		_, err := client.Namespace(item.GetNamespace()).
			Patch(ctx, item.GetName(), types.MergePatchType, []byte("{}"), metav1.PatchOptions{})

		if err != nil && !apierrs.IsNotFound(err) {
			return fmt.Errorf("unable to patch resource %s/%s (gvr: %s) - %w",
				item.GetNamespace(), item.GetName(),
				gvr, err)
		}

		return nil
	}

	pager := pager.New(listFunc)
	return pager.EachListItem(ctx, metav1.ListOptions{}, onEach)
}

func storageVersion(crd *apix.CustomResourceDefinition) string {
	var version string

	for _, v := range crd.Spec.Versions {
		if v.Storage {
			version = v.Name
			break
		}
	}

	return version
}

const (
	IgnoreNotFoundEnvKey = "IGNORE_NOT_FOUND"
)

// Run runs the storage-version migration command using the provided flag set
// and arguments. It exits the process if migration fails.
func Run(flags *flag.FlagSet, args []string) {
	logger := setupLogger()
	defer logger.Sync()

	env := environment.ClientConfig{}
	env.InitFlags(flags)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Fatal(err)
	}

	config, err := env.GetRESTConfig()
	if err != nil {
		logger.Fatalf("failed to get kubeconfig %s", err)
	}

	grs, err := parseResources(flags.Args())
	if err != nil {
		logger.Fatal(err)
	}

	ignoreNotFound, _ := strconv.ParseBool(os.Getenv(IgnoreNotFoundEnvKey))

	migrator := NewMigrator(
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
