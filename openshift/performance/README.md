
# Example run on OCP

```bash

# images are meant to be built on CI, however any user built image can be passed as follows:

export KNATIVE_SERVING_DATAPLANE_PROBE=docker.io/skonto/dataplane-probe-13daa01eab9bbd0b55b029ef0217990f@sha256:38888e57c2eaba6de9689bde4313904af022161ac659617334e5605b15af4286
export KNATIVE_SERVING_REAL_TRAFFIC_TEST=docker.io/skonto/real-traffic-test-0d6cfd702f7100116b002498a1c9d449@sha256:7109b39aa4325b0c86a1b4251a672abc0e3dc362d14e32ad1886f762942ab410
export KNATIVE_SERVING_ROLLOUT_PROBE=docker.io/skonto/rollout-probe-16b878ae522fca2c6d0a486b4be446cd@sha256:224d53bbcf2021c41d25271ae32c41207e2f6fb44a9eb465d074126e3493fb2e
export KNATIVE_SERVING_LOAD_TEST=docker.io/skonto/load-test-16ad8813e1e519c16903611ab3798c1c@sha256:d98d359e8bd1e39615ae6f5226ed0ec90c40d8eb8dec8915da6e87054ea2d32d
export KNATIVE_SERVING_RECONCILIATION_DELAY=docker.io/skonto/reconciliation-delay-6074d88fac79c5d2be9fb1c4ae840488@sha256:6db2558e597a44fa4e735cd8a88c0183cffa18c04b4941ef9dd43687cf0cb5a8
export KNATIVE_SERVING_SCALE_FROM_ZERO=docker.io/skonto/scale-from-zero-9924dc8c7b18ccca4da8563a28b55a50@sha256:5b8807543da8ef4e43020e61e1cf7e37dfd90747882aeedd3999fbab76da4d04
export KNATIVE_SERVING_TEST_AUTOSCALE=docker.io/skonto/autoscale-c163c422b72a456bad9aedab6b2d1f13@sha256:02fc725cef343d41d2278322eef4dd697a6a865290f5afd02ff1a39213f4bbcb

export KNATIVE_SERVING_TEST_RUNTIME=docker.io/skonto/runtime-5fa7cf4c043dfad63fa28de0cfa3b264@sha256:ff5aece839ddec959ed4f2e32c61731ac8ea2550f29a63d73bce100a8a4b004e
export KNATIVE_SERVING_TEST_HELLOWORLD=docker.io/skonto/helloworld-edca531b677458dd5cb687926757a480@sha256:0c9589cde631d33be7548bf54b1e4dbd8e15e486bcd640e0a6c986c5bc1038a6
export KNATIVE_SERVING_TEST_SLOWSTART=docker.io/skonto/slowstart-754e95e646a3d72ab225ebdf3a77a410@sha256:c89ce2dc03377593cd63827b22d4a2f0406fd78870b8e2fff773936940a7efb1

# note that images can be build locally with `ko resolve ./test/performance/benchamrks/<path_to_yaml>` as usual.

# setup OpenSearch
helm repo add opensearch https://opensearch-project.github.io/helm-charts/
helm repo update
helm search repo opensearch
helm install my-deployment opensearch/opensearch
helm install dashboards opensearch/opensearch-dashboards

oc get po 
NAME                                                READY   STATUS    RESTARTS   AGE
dashboards-opensearch-dashboards-5977566fb9-p4v9m   1/1     Running   0          105m
opensearch-cluster-master-0                         1/1     Running   0          105m
opensearch-cluster-master-1                         1/1     Running   0          105m
opensearch-cluster-master-2                         1/1     Running   0          105m

export ES_HOST_PORT=opensearch-cluster-master.default.svc.cluster.local:9200
export ES_USERNAME=admin
export ES_PASSWORD=admin
export SYSTEM_NAMESPACE=knative-serving
export USE_OPEN_SEARCH=true

# optional
export SERVING=<path to serving repo, root dir>

# note if you want to run against an insecure ES instance just export ES_DEVELOPMENT=true
# run the tests
./openshift/performance/scripts/run-all-performance-tests.sh

# visualization

# on a separate terminal run
oc port-forward svc/opensearch-cluster-master 9200:9200

export ES_URL=https://localhost:9200

# Creates an index template for the data
./openshift/performance/visualization/setup-es-index.sh

# setup grafana in your cluster and import the dashboard under ./openshift/performance/visualization

```