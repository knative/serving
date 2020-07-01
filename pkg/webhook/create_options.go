
package webhook

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	scheme "k8s.io/client-go/kubernetes/scheme"
	rest "k8s.io/client-go/rest"
)

type pods struct {
	client rest.Interface
	ns     string
}

// newPods returns a Pods
func newPods(client rest.Interface, namespace string) *pods {
	return &pods{
		client: client,
		ns:     namespace,
	}
}

// Create takes the representation of a pod and creates it.  Returns the server's representation of the pod, and an error, if there is any.
func (c *pods) CreateWithOptions(ctx context.Context, pod *v1.Pod, opts metav1.CreateOptions) (result *v1.Pod, err error) {
	result = &v1.Pod{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("pods").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(pod).
		Do().
		Into(result)
	return
}
