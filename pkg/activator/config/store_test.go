/*
Copyright 2021 The Knative Authors

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

package config

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ltesting "knative.dev/pkg/logging/testing"
	tracingconfig "knative.dev/pkg/tracing/config"
)

var tracingConfig = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "knative-serving",
		Name:      "config-tracing",
	},
	Data: map[string]string{
		"backend": "none",
	},
}

func TestStore(t *testing.T) {
	logger := ltesting.TestLogger(t)
	store := NewStore(logger)
	store.OnConfigChanged(tracingConfig)

	ctx := store.ToContext(context.Background())
	cfg := FromContext(ctx)

	if got, want := cfg.Tracing.Backend, tracingconfig.None; got != want {
		t.Fatalf("Tracing.Backend = %v, want %v", got, want)
	}

	newConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "knative-serving",
			Name:      "config-tracing",
		},
		Data: map[string]string{
			"backend":         "zipkin",
			"zipkin-endpoint": "foo.bar",
		},
	}
	store.OnConfigChanged(newConfig)

	ctx = store.ToContext(context.Background())
	cfg = FromContext(ctx)

	if got, want := cfg.Tracing.Backend, tracingconfig.Zipkin; got != want {
		t.Fatalf("Tracing.Backend = %v, want %v", got, want)
	}
}

func BenchmarkStoreToContext(b *testing.B) {
	logger := ltesting.TestLogger(b)
	store := NewStore(logger)
	store.OnConfigChanged(tracingConfig)

	b.Run("sequential", func(b *testing.B) {
		for j := 0; j < b.N; j++ {
			store.ToContext(context.Background())
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				store.ToContext(context.Background())
			}
		})
	})
}
