package builder

import (
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

type RevisionBuilder struct {
	*v1alpha1.Revision
}

func (bldr RevisionBuilder) WithConcurrencyModel(cm v1alpha1.RevisionRequestConcurrencyModelType) RevisionBuilder {
	bldr.Revision.Spec.ConcurrencyModel = cm
	return bldr
}

func (bldr RevisionBuilder) WithReadyStatus(t *testing.T) RevisionBuilder {
	bldr.Revision.Status.InitializeConditions()
	bldr.Revision.Status.MarkContainerHealthy()
	bldr.Revision.Status.MarkResourcesAvailable()

	if !bldr.Revision.Status.IsReady() {
		t.Fatalf("Wanted ready revision: %v", bldr.Revision)
	}

	return bldr
}

func (bldr RevisionBuilder) WithFailedStatus() RevisionBuilder {
	bldr.Revision.Status.InitializeConditions()
	bldr.Revision.Status.MarkContainerMissing("It's the end of the world as we know it")
	return bldr
}

func (bldr RevisionBuilder) WithCondition(c v1alpha1.RevisionCondition) RevisionBuilder {
	bldr.Revision.Status.Conditions = append(bldr.Revision.Status.Conditions, c)
	return bldr
}

func (bldr RevisionBuilder) Build() *v1alpha1.Revision {
	return bldr.Revision
}
