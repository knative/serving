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

package autoscaler

import (
	"errors"
	"testing"
	"time"

	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	"knative.dev/pkg/kmp"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/fake"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

var (
	existingNamespace    = "existing"
	nonExistingNamespace = "non-existing"
)

func TestGetMetricByName(t *testing.T) {
	type args struct {
		name types.NamespacedName
		info provider.CustomMetricInfo
	}
	tests := []struct {
		name    string
		args    args
		want    int64
		wantErr bool
	}{{
		name: "all good",
		args: args{
			name: types.NamespacedName{Namespace: existingNamespace, Name: "test"},
			info: concurrencyMetricInfo,
		},
		want: 11,
	}, {
		name: "all good (RPS)",
		args: args{
			name: types.NamespacedName{Namespace: existingNamespace, Name: "test"},
			info: rpsMetricInfo,
		},
		want: 14,
	}, {
		name: "requesting unsupported metric",
		args: args{
			name: types.NamespacedName{Namespace: existingNamespace, Name: "test"},
			info: provider.CustomMetricInfo{
				GroupResource: v1alpha1.Resource("services"),
				Namespaced:    true,
				Metric:        autoscaling.Concurrency,
			},
		},
		wantErr: true,
	}, {
		name: "error from metric client",
		args: args{
			name: types.NamespacedName{Namespace: nonExistingNamespace, Name: "test"},
			info: concurrencyMetricInfo,
		},
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewMetricProvider(staticMetrics(10.3, 14))
			got, err := p.GetMetricByName(tt.args.name, tt.args.info, labels.Everything())
			if (err != nil) != tt.wantErr {
				t.Errorf("GetMetricByName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			gotValue, _ := got.Value.AsInt64()
			if gotValue != tt.want {
				t.Errorf("GetMetricByName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMetricBySelector(t *testing.T) {
	provider := NewMetricProvider(staticMetrics(10.0, 14))
	_, got := provider.GetMetricBySelector("foo", labels.NewSelector(), concurrencyMetricInfo, labels.Everything())
	if got != errNotImplemented {
		t.Errorf("GetMetricBySelector() = %v, want %v", got, errNotImplemented)
	}

	_, got = provider.GetMetricBySelector("foo", labels.NewSelector(), rpsMetricInfo, labels.Everything())
	if got != errNotImplemented {
		t.Errorf("GetMetricBySelector() = %v, want %v", got, errNotImplemented)
	}
}

func TestListAllMetrics(t *testing.T) {
	provider := NewMetricProvider(staticMetrics(10.0, 14))
	gotConcurrency := provider.ListAllMetrics()[0]

	if equal, err := kmp.SafeEqual(gotConcurrency, concurrencyMetricInfo); err != nil {
		t.Errorf("Got error comparing output, err = %v", err)
	} else if !equal {
		t.Errorf("ListAllMetrics() = %v, want %v", gotConcurrency, concurrencyMetricInfo)
	}

	gotRPS := provider.ListAllMetrics()[1]
	if equal, err := kmp.SafeEqual(gotRPS, rpsMetricInfo); err != nil {
		t.Errorf("Got error comparing output, err = %v", err)
	} else if !equal {
		t.Errorf("ListAllMetrics() = %v, want %v", gotRPS, rpsMetricInfo)
	}
}

func staticMetrics(concurrency, rps float64) MetricClient {
	return &fake.MetricClient{
		StableConcurrency: concurrency,
		StableRPS:         rps,
		ErrF: func(key types.NamespacedName, now time.Time) error {
			if key.Namespace != existingNamespace {
				return errors.New("doesn't exist")
			}
			return nil
		},
	}
}
