/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/gc"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	logger := logtesting.TestLogger(t)
	store := NewStore(logger, 10*time.Hour)

	gcConfig := ConfigMapFromTestFile(t, "config-gc")

	store.OnConfigChanged(gcConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("revision-gc", func(t *testing.T) {
		expected, _ := gc.NewConfigFromConfigMapFunc(logger, 10*time.Hour)(gcConfig)
		if diff := cmp.Diff(expected, config.RevisionGC); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})
}
