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

	"go.uber.org/atomic"
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

func TestRateLimitGlobal(t *testing.T) {
	logger := logtesting.TestLogger(t)

	var resolves atomic.Int32
	var resolver resolveFunc = func(ctx context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
		resolves.Inc()
		return "", errors.New("failed")
	}

	queue := workqueue.NewRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(1*time.Second, 5*time.Second))
	subject := newBackgroundResolver(logger, resolver, queue, func(types.NamespacedName) {})

	stop := make(chan struct{})
	done := subject.Start(stop, 1)

	defer func() {
		close(stop)
		<-done
	}()

	for i := 0; i < 5; i++ {
		name := fmt.Sprint("rev", i)
		subject.Resolve(rev(name, name+"img1", name+"img2"), k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
	}

	// Rate limit base rate in this test is 1 second, so this should give time for at most 1 resolve.
	time.Sleep(500 * time.Millisecond)

	if r := resolves.Load(); r > 1 {
		t.Fatalf("Expected resolves to be rate limited, but was called %d times", r)
	}
}

func TestRateLimitPerItem(t *testing.T) {
	logger := logtesting.TestLogger(t)

	var resolves atomic.Int32
	var resolver resolveFunc = func(ctx context.Context, img string, _ k8schain.Options, _ sets.String) (string, error) {
		resolves.Inc()
		return "", errors.New("failed")
	}

	queue := workqueue.NewRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(50*time.Millisecond, 5*time.Second))

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

	start := time.Now()
	for i := 0; i < 4; i++ {
		subject.Clear(types.NamespacedName{Name: "rev", Namespace: "ns"})
		resolution, err := subject.Resolve(rev("rev", "img1", "img2"), k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err != nil || resolution != nil {
			t.Fatalf("Expected Resolve to be nil, nil but got %v, %v", resolution, err)
		}

		<-enqueue
		resolution, err = subject.Resolve(rev("rev", "img1", "img2"), k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
		if err == nil {
			t.Fatalf("Expected Resolve to fail")
		}
	}

	if took := time.Since(start); took < 600*time.Millisecond {
		// Per-item time is 50ms, so after 4 cycles of back-off should take at least 600ms.
		// (Otherwise will take only ~200ms)
		t.Fatal("Expected second resolve to take longer than 600ms, but took", took)
	}

	subject.Forget(types.NamespacedName{Name: "rev", Namespace: "ns"})

	start = time.Now()
	resolution, err := subject.Resolve(rev("rev", "img1", "img2"), k8schain.Options{ServiceAccountName: "san"}, sets.NewString("skip"), 0)
	if err != nil || resolution != nil {
		t.Fatalf("Expected Resolve to be nil, nil but got %v, %v", resolution, err)
	}

	<-enqueue
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatal("Expected Forget to remove revision from rate limiter, but took", took)
	}
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
			},
		},
	}
}
