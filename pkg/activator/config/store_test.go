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
	netcfg "knative.dev/networking/pkg/config"
	ltesting "knative.dev/pkg/logging/testing"
)

var networkingConfig = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name: netcfg.ConfigMapName,
	},
	Data: map[string]string{
		"ingress-class": "random.ingress.networking.knative.dev",
	},
}

func TestStore(t *testing.T) {
	logger := ltesting.TestLogger(t)
	store := NewStore(logger)
	store.OnConfigChanged(networkingConfig)

	ctx := store.ToContext(context.Background())
	cfg := FromContext(ctx)

	if got, want := cfg.Network.DefaultIngressClass, "random.ingress.networking.knative.dev"; got != want {
		t.Fatalf("Networking.In = %v, want %v", got, want)
	}

	newConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: netcfg.ConfigMapName,
		},
		Data: map[string]string{
			"ingress-class": "new-ingress-class",
		},
	}
	store.OnConfigChanged(newConfig)

	ctx = store.ToContext(context.Background())
	cfg = FromContext(ctx)

	if got, want := cfg.Network.DefaultIngressClass, "new-ingress-class"; got != want {
		t.Fatalf("Tracing.Backend = %v, want %v", got, want)
	}
}

func BenchmarkStoreToContext(b *testing.B) {
	logger := ltesting.TestLogger(b)
	store := NewStore(logger)
	store.OnConfigChanged(networkingConfig)

	b.Run("sequential", func(b *testing.B) {
		for b.Loop() {
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
