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
	"math"
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
	"k8s.io/client-go/util/workqueue"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var (
	errDigest    = errors.New("digest error")
	fakeRevision = rev("rev", "first-image", "second-image")
)

func TestResolveInBackground(t *testing.T) {
	tests := []struct {
		name                      string
		resolver                  resolveFunc
		timeout                   *time.Duration
		wantStatuses              []v1.ContainerStatus
		wantInitContainerStatuses []v1.ContainerStatus
		wantError                 error
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
		wantInitContainerStatuses: []v1.ContainerStatus{{
			Name:        "first-init",
			ImageDigest: "init-digest",
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
		wantInitContainerStatuses: []v1.ContainerStatus{{
			Name:        "first-init",
			ImageDigest: "init-san-skip",
		}},
	}, {
		name: "one slow resolve",
		resolver: func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
			if img == "first-image" {
				// make the first resolve arrive after the second.
				time.Sleep(50 * time.Millisecond)
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
		wantInitContainerStatuses: []v1.ContainerStatus{{
			Name:        "first-init",
			ImageDigest: "init-digest",
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
				select {
				case <-time.After(10 * time.Second):
				case <-ctx.Done():
				}
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
			subject := newBackgroundResolver(logger, tt.resolver, workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()), cb)

			stop := make(chan struct{})
			done := subject.Start(stop, 10)

			defer func() {
				close(stop)
				<-done
			}()

			for i := 0; i < 2; i++ {
				t.Run(fmt.Sprint("iteration", i), func(t *testing.T) {
					logger := logtesting.TestLogger(t)
					initContainerStatuses, statuses, err := subject.Resolve(logger, fakeRevision, k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), timeout)
					if err != nil || statuses != nil || initContainerStatuses != nil {
						// Initial result should be nil, nil, nil since we have nothing in cache.
						t.Errorf("Resolve() = %v, %v %v, wanted nil, nil, nil", statuses, initContainerStatuses, err)
					}

					select {
					case <-ready:
						// got the result.
					case <-time.After(2 * time.Second):
						// This shouldn't happen in these tests. The callback is always
						// eventually called.
						t.Fatalf("Resolver did not report ready")
					}

					initContainerStatuses, statuses, err = subject.Resolve(logger, fakeRevision, k8schain.Options{}, nil, timeout)
					if got, want := err, tt.wantError; !errors.Is(got, want) {
						t.Errorf("Resolve() = _, %q, wanted %q", got, want)
					}

					if got, want := statuses, tt.wantStatuses; !reflect.DeepEqual(got, want) {
						t.Errorf("Resolve() = %v, wanted %v", got, want)
					}

					if got, want := initContainerStatuses, tt.wantInitContainerStatuses; !reflect.DeepEqual(got, want) {
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

func TestRateLimitPerItem(t *testing.T) {
	logger := logtesting.TestLogger(t)

	var resolver resolveFunc = func(_ context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
		if img == "img1" || img == "init" {
			return "", nil
		}

		return "", errors.New("failed")
	}

	baseDelay := 50 * time.Millisecond
	queue := workqueue.NewRateLimitingQueue(newItemExponentialFailureRateLimiter(baseDelay, 5*time.Second))

	enqueue := make(chan struct{})
	subject := newBackgroundResolver(logger, resolver, queue, func(types.NamespacedName) {
		enqueue <- struct{}{}
	})

	stop := make(chan struct{})
	done := subject.Start(stop, 1)

	defer func() {
		close(stop)
		<-done
	}()

	revision := rev("rev", "img1", "img2")
	for i := 0; i < 3; i++ {
		subject.Clear(types.NamespacedName{Name: revision.Name, Namespace: revision.Namespace})
		start := time.Now()
		initResolution, resolution, err := subject.Resolve(logger, revision, k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err != nil || resolution != nil || initResolution != nil {
			t.Fatalf("Expected Resolve to be nil, nil, nil but got %v, %v, %v", resolution, initResolution, err)
		}

		<-enqueue

		_, _, err = subject.Resolve(logger, revision, k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err == nil {
			t.Fatalf("Expected Resolve to fail")
		}

		latency := time.Since(start)
		// no delay on first resolve
		expected := time.Duration(math.Pow(2, float64(i-1))) * baseDelay
		if latency < expected {
			t.Fatalf("latency = %s, want at least %s", latency, expected)
		}
	}

	t.Run("Does not affect other revisions", func(t *testing.T) {
		start := time.Now()
		_, resolution, err := subject.Resolve(logger, rev("another-revision", "img1", "img2"), k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err != nil || resolution != nil {
			t.Fatalf("Expected Resolve to be nil, nil but got %v, %v", resolution, err)
		}

		<-enqueue
		if took := time.Since(start); took > baseDelay*2*2 {
			t.Fatal("Expected per-item limit not to affect other revisions, but took", took)
		}
	})

	t.Run("Forget clears per-item rate limit", func(t *testing.T) {
		subject.Forget(types.NamespacedName{Name: revision.Name, Namespace: revision.Namespace})

		start := time.Now()
		_, resolution, err := subject.Resolve(logger, revision, k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err != nil || resolution != nil {
			t.Fatalf("Expected Resolve to be nil, nil but got %v, %v", resolution, err)
		}

		<-enqueue
		// no delay on first resolve
		if took := time.Since(start); took > (baseDelay * 2) {
			t.Fatal("Expected Forget to remove revision from rate limiter, but took", took)
		}
	})
}

type resolveFunc func(context.Context, string, k8schain.Options, sets.String) (string, error)

func (r resolveFunc) Resolve(c context.Context, s string, o k8schain.Options, t sets.String) (string, error) {
	return r(c, s, o, t)
}

func rev(name, firstImage, secondImage string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "ns",
		},
		Spec: v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "first",
					Image: firstImage,
				}, {
					Name:  "second",
					Image: secondImage,
				}},
				InitContainers: []corev1.Container{{
					Name:  "first-init",
					Image: "init",
				}},
			},
		},
	}
}
