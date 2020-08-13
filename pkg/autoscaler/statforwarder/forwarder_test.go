package statforwarder

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/sets"
	fakekube "k8s.io/client-go/kubernetes/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	rtesting "knative.dev/pkg/reconciler/testing"
	_ "knative.dev/pkg/system/testing"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	testIP = "1.23.456.789"
)

var (
	testBs = hash.NewBucketSet(sets.NewString("as-bucket-00-of-02, as-bucket-01-of-02"))
)

func nonOp(sm asmetrics.StatMessage) {}

func newForwarder() *Forwarder {
	return New(context.Background(), fakekube.NewSimpleClientset(), testIP, testBs, nonOp, zap.NewNop().Sugar())
}

func TestForwarderReconcile(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	fakeendpointsinformer.Get(ctx)
	fakeservice := fakeserviceinformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), fakeservice.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	t.Cleanup(func() {
		cancel()
		waitInformers()
	})

	New(ctx, fakekube.NewSimpleClientset(), testIP, testBs, nonOp, zap.NewNop().Sugar())
}
