# 2018 API Core Roadmap

The purpose of the API Core group is to implement the control plane API for the
Elafros project.  This includes the API governance process as well as
implementation and supporting documentation.

This roadmap is what we hope to accomplish in 2018.


## References

* [Resource Overview](../../docs/spec/overview.md)
* [Conformance Tests](../../test/conformance/README.md)

In 2018, we will largely focus on curating and implementing the Elafros resource
specification.

## Areas of Interest and Requirements

1. **Process**.  It must be clear to contributors how to drive changes to the
  Elafros API.
1. **Schema**.  [The Elafros API schema](../../docs/spec/spec.md) matches
  [our implementation.](./ela/).
1. **Semantics**.  The [semantics](../controller/) of Elafros API interactions
  match [our specification](../../docs/spec/normative_examples.md), and are well
  covered by [conformance testing](../../test/conformance/README.md).

<!-- TODO(mattmoor): Should this cover Infrastructure as well? -->

### Process

1. **Define the process** by which changes to the API are proposed, approved,
  and implemented.
1. **Define our conventions** to which API changes should adhere for consistency
  with the Elafros API.

### Specification

1. **Complete our implementation** of the initial API specification.

1. **Track changes** to our API specification (according to our process) over
  time, including the versioning of API resources.

1. **Triage drift** of our implementation from the API specification.

<!-- TODO(mattmoor): Should this include something about webhook validation? -->


### Semantics

1. **Implement our desired semantics** as outlined in our
  ["normative examples"](../../docs/spec/normative_examples.md).

1. **Fail gracefully and clearly** as outlined in our
  ["errors conditions and reporting"](../../docs/spec/errors.md) docs.
<!-- TODO(mattmoor): https://github.com/elafros/elafros/issues/459 -->

1. **Ensure continued conformance** of our implementation with the API
  specification over time by ensuring semantics are well covered by our
  conformance testing.
<!-- TODO(mattmoor): https://github.com/elafros/elafros/issues/234 -->
<!-- TODO(mattmoor): https://github.com/elafros/elafros/issues/492 -->

1. **Operator Extensions**.  Guidelines for how operators can/should customize
an Elafros installation (e.g. runtime contract) are captured in documentation.

<!-- ## What We Are Not Doing -->

