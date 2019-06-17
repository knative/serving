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
	"strings"
	"testing"

	"github.com/knative/pkg/kmp"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"

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
			p := NewMetricProvider(staticConcurrency(10.3))
			got, err := p.GetMetricByName(tt.args.name, tt.args.info)
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
	provider := NewMetricProvider(staticConcurrency(10.0))
	_, got := provider.GetMetricBySelector("foo", labels.NewSelector(), concurrencyMetricInfo)
	if got != errNotImplemented {
		t.Errorf("GetMetricBySelector() = %v, want %v", got, errNotImplemented)
	}
}

func TestListAllMetrics(t *testing.T) {
	provider := NewMetricProvider(staticConcurrency(10.0))
	got := provider.ListAllMetrics()[0]

	if equal, err := kmp.SafeEqual(got, concurrencyMetricInfo); err != nil {
		t.Errorf("Got error comparing output, err = %v", err)
	} else if !equal {
		t.Errorf("ListAllMetrics() = %v, want %v", got, concurrencyMetricInfo)
	}
}

type staticConcurrency float64

func (s staticConcurrency) StableAndPanicConcurrency(key string) (float64, float64, error) {
	if strings.HasPrefix(key, existingNamespace) {
		return (float64)(s), 0.0, nil
	}
	return 0.0, 0.0, errors.New("doesn't exist")
}
