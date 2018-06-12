# Design goals

* Same API to handle applications as well as functions (serverless)
* Simple enough to get started but will handle complex rollout and traffic management that we’ve seen in practice
* Gradual complexity increase/learn as you use it
* Familiar to existing k8s users
* Principled abstractions that can be reused by higher levels
* Clear separation of concerns
* Declarative - grows with users needs (CI/CD, tooling, etc.)
* Reuse everything we can
