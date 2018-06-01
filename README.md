# Welcome, Knative

With Kubernetes and Istio rapidly becoming a universal platform for running
cloud-native software, there is a growing need to codify best practices and
identify common patterns that are shared across successful Kubernetes- and
Istio-based systems.

Knative (pronounced /ˈnā-tiv/) addresses that need by delivering opinionated
middleware for building modern, source-centric, container-based applications
that scale efficiently when faced with elastic demand. 

Knative components work on top of Kubernetes and Istio to standardize and
simplify many of the common developer tasks, such as:

- orchestrating source-to-container workflows
- routing and managing traffic during deployment
- scaling and sizing resources based on demand
- binding running services to eventing ecosystems

This open source project directs many expert eyes to the
difficult-but-non-differentiated parts of building modern cloud-native
applications, allowing developers to focus instead on the interesting bits that
set their teams apart.


# What are the Knative components?

Currently, Knative consists of the following top-level repositories:

- [build](https://github.com/knative/build) and
  [build-templates](https://github.com/knative/build-templates)
- [serving](https://github.com/knative/serving)
- [eventing](https://github.com/knative/eventing)

We expect this list to grow as more areas are identified.


# How do I use Knative?

You can install and run pre-built Knative components directly into your project
by following the instructions at https://github.com/knative/install.


# Who is Knative for?

**Developers**

 > *TODO: Insert user personas diagram here*

Knative components offer Kubernetes-native APIs for deploying functions,
applications, and containers to an auto-scaling runtime.

To get started as a developer, [pick a Kubernetes cluster of your
choice](https://kubernetes.io/docs/setup/pick-right-solution/) and follow the
[Knative installation instructions](https://github.com/knative/install) to get
the system up and running and [run some sample code](./sample/README.md).

Once the Knative components are available, you can build and deploy your own
applications. For example:

> *TODO: Insert minimal hello world example here*

To join the conversation, head over to
https://groups.google.com/d/forum/knative-users.

**Operators**

Knative components are intended to be integrated into more polished products
that cloud service providers or in-house teams in large enterprises can then
operate. 

Any enterprise or cloud provider can adopt Knative components into their own
systems and pass the benefits along to their customers.

> *TODO: Add more about the operator experience here*

**Contributors**

With a clear project scope, lightweight governance model and clean lines of
separation between pluggable components, the Knative project establishes an
efficient contributor workflow.

Knative is a diverse, open, and inclusive community. To get involved, see
[CONTRIBUTING.md](./CONTRIBUTING.md) and [DEVELOPMENT.md](./DEVELOPMENT.md) and
join the [#community](https://knative.slack.com/messages/C92U2C59P/) Slack
channel.

Your own path to becoming a Knative contributor can begin anywhere. Bug reports
and friction logs from new developers are especially welcome.