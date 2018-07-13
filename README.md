# Welcome, Knative

Knative (pronounced /ˈnā-tiv/) extends Kubernetes to provide the
missing building blocks that developers need to create modern,
source-centric, container-based, cloud-native applications.

Each of the components under the Knative project attempt to identify
common patterns and codify the best practices shared by successful
real-world Kubernetes-based frameworks and applications, such as:

- orchestrating source-to-container workflows
- routing and managing traffic during deployment
- scaling and sizing resources based on demand
- binding running services to eventing ecosystems

Knative focuses on the "boring but difficult" parts that everyone
needs, but no one benefits from doing over again on their own. This in
turn frees developers to spend more time writing application
code, not worrying about how they are going to build,
deploy, monitor, and debug it.


# What are the Knative components?

Currently, Knative consists of the following top-level repositories:

- [build](https://github.com/knative/build) and
    [build-templates](https://github.com/knative/build-templates) —
    automatic, repeatable server-side container builds
- [serving](https://github.com/knative/serving) — scale to zero,
  request-driven compute
- [eventing](https://github.com/knative/eventing) — management and
  delivery of events

We expect this list to grow as more areas are identified.


# How do I use Knative?

You can choose to install individual Knative components following the
instructions in each repo, or install a pre-built suite of components
by following the instructions at
[docs/install](https://github.com/knative/docs/tree/master/install).

Read an [overview of the Knative Serving resource types](https://github.com/knative/serving/blob/master/docs/spec/overview.md#resource-types).


# Who is Knative for?

Knative is being created with several
[personas](./docs/product/personas.md) in mind.

**Developers**

Knative components offer Kubernetes-native APIs for deploying
functions, applications, and containers to an auto-scaling runtime.

To get started as a developer, [pick a Kubernetes cluster of your
choice](https://kubernetes.io/docs/setup/pick-right-solution/) and
follow the [Knative installation 
instructions](https://github.com/knative/docs/tree/master/install) 
to get the system up and running and [run some sample 
code](./sample/README.md). The install instructions also include a 
[sample application](https://github.com/knative/docs/blob/master/install/Knative-with-Minikube.md#test-app) 
which demonstrates some of the key features of Knative.

To join the conversation, head over to
https://groups.google.com/d/forum/knative-users.

**Operators**

Knative components are intended to be integrated into more polished
products that cloud service providers or in-house teams in large
enterprises can then operate.

Any enterprise or cloud provider can adopt Knative components into
their own systems and pass the benefits along to their customers.

**Contributors**

With a clear project scope, lightweight governance model and clean
lines of separation between pluggable components, the Knative project
establishes an efficient contributor workflow.

Knative is a diverse, open, and inclusive community. To get involved,
see [CONTRIBUTING.md](./CONTRIBUTING.md) and
[DEVELOPMENT.md](./DEVELOPMENT.md) and join the
[#community](https://knative.slack.com/messages/C92U2C59P/) Slack
channel.

Your own path to becoming a Knative contributor can [begin
anywhere](https://github.com/knative/serving/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22).
[Bug reports](https://github.com/knative/serving/issues/new) and
friction logs from new developers are especially welcome.
