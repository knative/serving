# Feature Lifecycle Checklist

This is the checklist describing the essential requirements for a feature to
be labeled as Alpha, Beta, or Stable. This is a shortened version of the full
[Feature Lifecycle](FEATURE-LIFECYCLE.md) document.

- [Alpha](#alpha)
- [Beta](#beta)
- [Stable](#stable)

## Alpha

A development feature can be officially labeled as Alpha once it meets the following
requirements:

* *Config*: Requires explicit user action to enable (e.g. a config field, config resource, or installation action).

* *Docs*: Reference docs are published on istio.io.

* *Docs*: Basic feature docs are published on istio.io describing what the feature does, how to use it, and any caveats.

* *Docs*: A reference to the design doc / issue is published on istio.io.

* *Tests*: Test coverage is 90%.

* *Tests*: Integration tests cover core use cases with the feature enabled.

* *Tests*: When disabled the feature does not affect system stability or performance.

* *Performance*: Performance requirements assessed as part of design.

* *API* (Optional): Initial API review.

## Beta

An Alpha feature can be officially labeled as Beta once it meets the following additional requirements:

* *API*: API has had a thorough API review and is thought to be complete.

* *CLI*: Necessary CLI commands have been implemented and are complete.

* *Config*: Can be enabled by default without requiring explicit user action.

* *Bugs*: Feature has no outstanding P0 bugs.

* *Docs*: Performance expectations of the feature are documented, may have caveats.

* *Docs*: Documentation on istio.io includes samples/tutorials

* *Docs*: Documentation on istio.io includes appropriate glossary entries

* *Tests*: Test coverage is 95%

* *Tests*: Integration tests cover feature edge cases

* *Tests*: End-to-end tests cover samples/tutorials

* *Tests*: Fixed issues have tests to prevent regressions

* *Performance*: Feature has baseline performance tests.

## Stable

A Beta feature can be officially labeled as Stable once it meets the following additional requirements:

* *Bugs*: Feature has no outstanding P0 or P1 bugs.

* *Perf*: Performance (latency/scale) is quantified and documented.

* *Tests*: Automated tests are in place to prevent performance regressions.
