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

package revision

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var (
	errDigest    = errors.New("digest error")
	fakeRevision = &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rev",
			Namespace: "ns",
		},
		Spec: v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "first",
					Image: "first-image",
				}, {
					Name:  "second",
					Image: "second-image",
				}},
			},
		},
	}
)

func TestResolveInBackground(t *testing.T) {
	tests := []struct {
		name         string
		resolver     resolveFunc
		timeout      *time.Duration
		wantStatuses []v1.ContainerStatus
		wantError    error
	}{{
		name: "success",
		resolver: func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			return img + "-digest", nil
		},
		wantStatuses: []v1.ContainerStatus{{
			Name:        "first",
			ImageDigest: "first-image-digest",
		}, {
			Name:        "second",
			ImageDigest: "second-image-digest",
		}},
	}, {
		name: "passing params",
		resolver: func(_ context.Context, img string, opt k8schain.Options, skip sets.String) (string, error) {
			return fmt.Sprintf("%s-%s-%s", img, opt.ServiceAccountName, skip.List()[0]), nil
		},
		wantStatuses: []v1.ContainerStatus{{
			Name:        "first",
			ImageDigest: "first-image-san-skip",
		}, {
			Name:        "second",
			ImageDigest: "second-image-san-skip",
		}},
	}, {
		name: "one slow resolve",
		resolver: func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			if img == "first-image" {
				// make the first resolve arrive after the second.
				time.Sleep(200 * time.Millisecond)
			}
			return img + "-digest", nil
		},
		wantStatuses: []v1.ContainerStatus{{
			Name:        "first",
			ImageDigest: "first-image-digest",
		}, {
			Name:        "second",
			ImageDigest: "second-image-digest",
		}},
	}, {
		name: "resolver entirely fails",
		resolver: func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			return img + "-digest", errDigest
		},
		wantError: errDigest,
	}, {
		name: "resolver fails one image",
		resolver: func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			if img == "second-image" {
				return "", errDigest
			}

			return img + "-digest", nil
		},
		wantError: errDigest,
	}, {
		name:    "timeout",
		timeout: ptr.Duration(10 * time.Millisecond),
		resolver: func(ctx context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			if img == "second-image" {
				time.Sleep(500 * time.Millisecond)
			}

			return img + "-digest", ctx.Err()
		},
		wantError: context.DeadlineExceeded,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout := 5 * time.Second
			if tt.timeout != nil {
				timeout = *tt.timeout
			}

			ready := make(chan types.NamespacedName)
			cb := func(rev types.NamespacedName) {
				ready <- rev
				close(ready)
			}

			logger := logtesting.TestLogger(t)
			subject := newBackgroundResolver(logger, tt.resolver, cb)

			stop := make(chan struct{})
			done := subject.Start(stop, 10)

			defer func() {
				close(stop)
				<-done
			}()

			for i := 0; i < 2; i++ {
				t.Run(fmt.Sprintf("iteration %d", i), func(t *testing.T) {
					statuses, err := subject.Resolve(fakeRevision, k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), timeout)
					if err != nil || statuses != nil {
						// Initial result should be nil, nil since we have nothing in cache.
						t.Errorf("Resolve() = %v, %v, wanted nil, nil", statuses, err)
					}

					select {
					case <-ready:
						// got the result.
					case <-time.After(2 * time.Second):
						// This shouldn't happen in these tests. The callback is always
						// eventually called.
						t.Fatalf("Resolver did not report ready")
					}

					statuses, err = subject.Resolve(fakeRevision, k8schain.Options{}, nil, timeout)
					if got, want := err, tt.wantError; !errors.Is(got, want) {
						t.Errorf("Resolve() = _, %q, wanted %q", got, want)
					}
					if got, want := statuses, tt.wantStatuses; !reflect.DeepEqual(got, want) {
						t.Errorf("Resolve() = %v, wanted %v", got, want)
					}

					// Clear, then we'll loop and make sure that we look everything up from scratch.
					subject.Clear(types.NamespacedName{Namespace: fakeRevision.Namespace, Name: fakeRevision.Name})
					ready = make(chan types.NamespacedName)
				})
			}
		})
	}
}

type resolveFunc func(context.Context, string, k8schain.Options, sets.String) (string, error)

func (r resolveFunc) Resolve(c context.Context, s string, o k8schain.Options, t sets.String) (string, error) {
	return r(c, s, o, t)
}
