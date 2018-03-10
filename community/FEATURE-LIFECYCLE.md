# Feature Lifecycle

This table describes the requirements for a feature to
be labeled as Alpha, Beta, or Stable. You can find a checklist rendition
of this information at [Feature Lifecycle Checklist](FEATURE-LIFECYCLE-CHECKLIST.md).

<table>
  <tr>
    <td></td>
    <td>Development (pre-alpha)</td>
    <td>Alpha</td>
    <td>Beta</td>
    <td>Stable</td>
  </tr>
  <tr>
    <td>Purpose</td>
    <td>Active development of a feature, not for use by end users directly.</td>
    <td>Used to get feedback on a design or feature or see how a tentative design performs, etc. Targeted at developers and expert users.</td>
    <td>Used to vet a solution in production without committing to it in the long term, to assess its viability, performance, usability, etc. Targetted at all users.</td>
    <td>Dependable, production hardened</td>
  </tr>
  <tr>
    <td>Stability</td>
    <td>Unknown</td>
    <td>May be buggy. Using the feature may expose bugs. Not active by default.</td>
    <td>Code is well tested. The feature is safe for production use.</td>
    <td>Code is well tested and stable. Safe for widespread deployment in production.</td>
  </tr>
  <tr>
    <td>Support</td>
    <td>None</td>
    <td>No guarantees on backward compatibility. The feature may be dropped at any time without notice.</td>
    <td>The overall feature will not be dropped, though details may change.</td>
    <td>The feature will appear in released software for many subsequent versions.</td>
  </tr>
  <tr>
    <td>Performance</td>
    <td>Unknown</td>
    <td>Performance requirements are assessed as part of design.</td>
    <td>Performance and scalability are characterized, but may have caveats.</td>
    <td>Perf (latency/scale) is quantified and documented, with guarantees against regression.</td>
  </tr>
  <tr>
    <td>Deprecation Policy</td>
    <td>None</td>
    <td>None</td>
    <td>Weak - 3 months</td>
    <td>Firm - 1 year</td>
  </tr>
  <tr>
    <td>Versioning</td>
    <td>The API version name contains dev (e.g. v1dev1)</td>
    <td>The API version name contains alpha (e.g. v1alpha1)</td>
    <td>The API version name contains beta (e.g. v2beta3)</td>
    <td>The API version is vX where X is an integer (e.g. v1)</td>
  </tr>
  <tr>
    <td>Availability</td>
    <td>The feature may or may not be available in the main Elafros repo.
The feature may or may not appear in an Elafros release. If it does appear in an Elafros release it will be disabled by default.
The feature requires an explicit flag to enable in addition to any configuration required to use the feature, in order to turn on dev features.</td>
    <td>The feature is committed to the Elafros repo
The feature appears in an official Elafros release
The feature requires explicit user action to use, e.g. a flag flip, use of a config resource, an explicit installation action, or an API being called.
When a feature is disabled it must not affect system stability.
Note: While Elafros is pre-1.0, some alpha features are enabled by default. Post 1.0 alpha features will require explicit action.</td>
    <td>In official Elafros releases
Enabled by default</td>
    <td>Same as Beta</td>
  </tr>
  <tr>
    <td>Audience</td>
    <td>Other developers closely collaborating on a feature or proof-of-concept.</td>
    <td>The feature is targeted at developers and expert users interested in giving early feedback</td>
    <td>Users interested in providing feedback on features</td>
    <td>All users</td>
  </tr>
  <tr>
    <td>Completeness</td>
    <td>Varies</td>
    <td>Some API operations or CLI commands may not be implemented
The feature need not have had an API review (an intensive and targeted review of the API, on top of a normal code review)</td>
    <td>All API operations and CLI commands should be implemented
End-to-end tests are complete and reliable
The API has had a thorough API review and is thought to be complete, though use during beta frequently turns up API issues not thought of during review</td>
    <td>Same as Beta</td>
  </tr>
  <tr>
    <td>Documentation</td>
    <td>Dev features are hidden in auto-generated reference docs.</td>
    <td>Alpha features are marked alpha in auto-generated reference docs.
Basic documentation describing what the feature does, how to enable it, the restrictions and caveats, and a pointer to the issue or design doc the feature is based on.</td>
    <td>Complete feature documentation published to elafros.dev
In addition to the basic alpha-level documentation, beta documentation includes samples, tutorials, glossary entries, etc.</td>
    <td>Same as Beta</td>
  </tr>
  <tr>
    <td>Upgradeability</td>
    <td>None</td>
    <td>The schema and semantics of a feature may change in a later software release, without any provision for preserving configuration objects in an existing elafros installation
API versions can increment faster than the monthly release cadence and the developer need not maintain multiple versions
Developers should still increment the API version when schema or semantics change in an incompatible way</td>
    <td>The schema and semantics may change in a later software release
When this happens, an upgrade path will be documented
In some cases, objects will be automatically converted to the new version
In other cases, a manual upgrade may be necessary
A manual upgrade may require downtime for anything relying on the new feature, and may require manual conversion of objects to the new version
When manual conversion is necessary, the project will provide documentation on the process</td>
    <td>Only strictly compatible changes are allowed in subsequent software releases</td>
  </tr>
  <tr>
    <td>Testing</td>
    <td>The presence of the feature must not affect any released features.</td>
    <td>The feature is covered by unit tests and integration tests where the feature is enabled and the tests are non-flaky.
Tests may not cover all corner cases, but the most common cases have been covered.
Testing code coverage at least 90%.
When the feature is disabled it does not regress performance of the system.</td>
    <td>Integration tests cover edge cases as well as common use cases.
Integration tests cover all issues reported on the feature.
The feature has end-to-end tests covering the samples/tutorials for the feature.
Test code coverage is at least 95%.</td>
    <td>Same as Beta, including tests for any issues discovered during Beta.</td>
  </tr>
  <tr>
    <td>Reliability</td>
    <td>None</td>
    <td>Because the feature is relatively new, and may lack complete end-to-end tests, enabling the feature via a flag might expose bugs which destabilize Elafros (e.g. a bug in a control loop might rapidly create excessive numbers of objects, exhausting API storage).</td>
    <td>Because the feature has e2e tests, enabling the feature should not create new bugs in unrelated features.
Because the feature is relatively new, it may have minor bugs.</td>
    <td>High. The feature is well tested and stable and reliable for all uses.</td>
  </tr>
  <tr>
    <td>Support</td>
    <td>None</td>
    <td>There is no commitment from Elafros to complete the feature
The feature may be dropped entirely in a later software release</td>
    <td>Elafros commits to complete the feature, in some form, in a subsequent Stable version
Typically this will happen within 3 months, but sometimes longer
Releases should simultaneously support two consecutive versions (e.g. v1beta1 and v1beta2; or v1beta2 and v1) for at least one supported release cycle (typically 3 months) so that users have enough time to upgrade and migrate resources.</td>
    <td>The feature will continue to be present for many subsequent releases.</td>
  </tr>
  <tr>
    <td>Recommended Use Cases</td>
    <td>Don't</td>
    <td>In short-lived development or testing environments, due to complexity of upgradeability and lack of long-term support.</td>
    <td>In development or testing environments.
In production environments as part of an evaluation of the feature in order to provide feedback.</td>
    <td>Any</td>
  </tr>
</table>
