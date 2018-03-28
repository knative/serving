# Elafros Personas

When discussing user actions, it is often helpful to [define specific
user roles](https://en.wikipedia.org/wiki/Persona_(user_experience)) who
might want to do the action.


## Elafros Compute

### Developer Personas

The developer personas are software engineers looking to build and run
a stateless application without concern about the underlying
infrastructure. Developers expect to have tools which integrate with
their native language tooling or business processes.

* Hobbyist
* Backend SWE
* Full stack SWE
* SRE

User stories:
* Deploy some code
* Update environment
* Roll back the last change
* Debug an error in code
* Monitor my application

### Operator Personas

The operator personas are focused on deploying and managing both
Elafros and the underlying Kubernetes cluster, as well as applying
organization policy and security patches.

* Hobbyist / Contributor
* Cluster administrator
* Security Engineer / Auditor
* Capacity Planner

User stories:
* Create an Elafros cluster
* Apply policy / RBAC
* Control or charge back for resource usage
* Choose logging or monitoring plugins
* Audit or patch running Revisions


## Elafros Build

We expect the build components of Elafros to be useful on their own,
as well as in conjunction with the compute components. 

### Developer

The developer personas for build are broader than the serverless
workloads that the Elafros compute product focuses on. Developers
expect to have build tools which integrate with their native language
tooling for managing dependencies and even detecting language and
runtime dependencies.

User stories:
* Start a build
* Read build logs

### Language operator / contributor

The language operators perform the work of integrating language
tooling into the Elafros build system. This role may work either
within a particular organization, or on behalf of a particular
language runtime.

User stories:
* Create a build image / build pack
* Enable build signing / provenance


## Elafros Events

Event generation and consumption is a core part of the serverless
(particularly function as a service) computing model. Event generation
and dispatch enables decoupling of event producers from consumers.

### Event consumer (developer)

An event consumer may be a software developer, or may be an integrator
which is reusing existing packaged functions to build a workflow
without writing code.

User stories:
* Determine what event sources are available
* Trigger my service when certain events happen (event binding)
* Filter events from a provider

### Event producer

An event producer owns a data source or system which produces events
which can be acted on by event consumers.

User stories:
* Publish events
* Control who can bind events


## Contributors

Contributors are an important part of the Elafros project. As such, we
will also consider how various infrastructure encourages and enables
contributors to the project, as well as the impact on end-users.

* Hobbyist or newcomer
* Motivated user
* Corporate (employed) maintainer
* Consultant

User stories:
* Check out the code
* Build and run the code
* Run tests
* View test status
* Run performance tests

