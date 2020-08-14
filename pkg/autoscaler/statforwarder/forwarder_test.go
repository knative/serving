package statforwarder

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	testIP  = "1.23.456.789"
	bucket1 = "as-bucket-00-of-02"
	bucket2 = "as-bucket-01-of-02"
)

var (
	testBs             = hash.NewBucketSet(sets.NewString(bucket1, bucket2))
	withHolderIdentify = map[string]string{resourcelock.LeaderElectionRecordAnnotationKey: fmt.Sprintf(`{"holderIdentity":"%s"}`, testIP)}
)

func nonOp(sm asmetrics.StatMessage) {}

func TestForwarderReconcile(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	service := fakeserviceinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(), service.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	t.Cleanup(func() {
		cancel()
		waitInformers()
	})

	New(ctx, kubeClient, testIP, testBs, nonOp, zap.NewNop().Sugar())

	// Create service could be triggered twice as the endpoints update triggers another
	// event and the service creation has been synced yet.
	created := make(chan struct{}, 2)
	kubeClient.PrependReactor("create", "services",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			created <- struct{}{}
			return false, nil, nil
		},
	)

	ns := system.Namespace()
	e := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:        bucket1,
			Namespace:   ns,
			Annotations: withHolderIdentify,
		},
	}
	kubeClient.CoreV1().Endpoints(ns).Create(e)
	endpoints.Informer().GetIndexer().Add(e)
	// Non-bucket endpoints should be skipped.
	select {
	case <-created:
		t.Error("Got services created, want no actions.")
	default:
	}

	// Wait for the servive to be created.
	select {
	case <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for service creation.")
	}

	// Check the endpoints get updated.
	got, err := endpoints.Lister().Endpoints(ns).Get(bucket1)
	if err != nil {
		t.Fatalf("Fail to get endpoints %s: %v", bucket1, err)
	}
	wantSubsets := []v1.EndpointSubset{{
		Addresses: []v1.EndpointAddress{{
			IP: testIP,
		}},
		Ports: []v1.EndpointPort{{
			Name: autoscalerPortName,
			Port: autoscalerPort,
		}}},
	}
	if !equality.Semantic.DeepEqual(wantSubsets, got.Subsets) {
		t.Errorf("Subsets = %v, want = %v", got.Subsets, wantSubsets)
	}
}

func TestForwarderSkipReconciling(t *testing.T) {
	ns := system.Namespace()
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	kubeClient := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	service := fakeserviceinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(), service.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	t.Cleanup(func() {
		cancel()
		waitInformers()
	})

	New(ctx, kubeClient, testIP, testBs, nonOp, zap.NewNop().Sugar())

	created := make(chan struct{})
	kubeClient.PrependReactor("create", "services",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			created <- struct{}{}
			return false, nil, nil
		},
	)

	testCases := []struct {
		description string
		namespace   string
		name        string
		annotations map[string]string
	}{{
		description: "non-bucket endpoints",
		namespace:   ns,
		name:        "activator",
		annotations: withHolderIdentify,
	}, {
		description: "different namespace",
		namespace:   "other-ns",
		name:        bucket1,
		annotations: withHolderIdentify,
	}, {
		description: "without annotation",
		namespace:   "otherns",
		name:        bucket1,
	}, {
		description: "invalid annotation",
		namespace:   "otherns",
		name:        bucket1,
		annotations: map[string]string{resourcelock.LeaderElectionRecordAnnotationKey: "invalid"},
	}, {
		description: "not the leader",
		namespace:   ns,
		name:        bucket1,
		annotations: map[string]string{resourcelock.LeaderElectionRecordAnnotationKey: `{"holderIdentity":"0.0.0.1"}`},
	}}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			e := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:        tc.name,
					Namespace:   tc.namespace,
					Annotations: tc.annotations,
				},
			}
			kubeClient.CoreV1().Endpoints(ns).Create(e)
			endpoints.Informer().GetIndexer().Add(e)

			select {
			case <-created:
				t.Error("Got services created, want no actions.")
			default:
			}
		})
	}
}
