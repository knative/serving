package statforwarder

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
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
	testBs         = hash.NewBucketSet(sets.NewString(bucket1, bucket2))
	holderIdentify = fmt.Sprintf(`{"holderIdentity":"%s"}`, testIP)
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

	ns := system.Namespace()
	e := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bucket1,
			Namespace: ns,
		},
	}

	created := make(chan struct{})
	kubeClient.PrependReactor("create", "service",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			created <- struct{}{}
			return false, nil, nil
		},
	)

	kubeClient.CoreV1().Endpoints(ns).Create(e)
	endpoints.Informer().GetIndexer().Add(e)

	select {
	case <-created:
		t.Error("Got created, want no actions.")
	default:
	}

	e.Annotations = map[string]string{resourcelock.LeaderElectionRecordAnnotationKey: holderIdentify}
	kubeClient.CoreV1().Endpoints(ns).Update(e)
	// endpoints.Informer().GetIndexer().Add(e)

	select {
	case <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for service creation.")
	}
}
