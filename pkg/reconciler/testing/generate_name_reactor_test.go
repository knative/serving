package testing

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgotesting "k8s.io/client-go/testing"
)

var deploymentsResource = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

func TestGenerateNameReactor(t *testing.T) {
	tests := []struct {
		name         string
		deployment   *appsv1.Deployment
		expectedName string
	}{{
		name:         "resource with name",
		expectedName: "basic",
		deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "basic",
			},
		},
	}, {
		name:         "resource with generatedName",
		expectedName: "fancy-00001",
		deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "fancy-",
			},
		},
	}, {
		name:         "resource with name and generatedName",
		expectedName: "fancy-00002",
		deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:         "fancy-00002",
				GenerateName: "fancy-",
			},
		},
	}, {
		name:         "broken resource with no names",
		expectedName: "",
		deployment:   &appsv1.Deployment{},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			lastHandlerInvoked := false

			fake := &clientgotesting.Fake{}
			fake.AddReactor("*", "*", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				lastHandlerInvoked = true
				return false, nil, nil
			})

			PrependGenerateNameReactor(fake)

			mutated := tc.deployment.DeepCopy()
			action := clientgotesting.NewCreateAction(deploymentsResource, "namespace", mutated)

			fake.Invokes(action, &appsv1.Deployment{})

			if diff := cmp.Diff(tc.expectedName, mutated.GetName()); diff != "" {
				t.Error(diff)
			}

			if !lastHandlerInvoked {
				t.Error("GenreateNameReactor should not interfere with the fake's ReactionChain")
			}
		})
	}
}
