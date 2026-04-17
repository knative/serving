# CI Issues Report

> Repository: knative/serving  
> Date: 2026-04-01  
> Branch: main

---

## Ranked Issues Table

| Rank | # | Issue | Severity | File | Line(s) | Difficulty | Fix |
|------|---|-------|----------|------|---------|------------|-----|
| 1 | I-07 | `gogo/protobuf` deprecated — unmaintained protobuf codegen | High | `hack/update-codegen.sh` | 47 | Hard | Migrate to `google/protobuf`; replace `--gogofaster_out=plugins=grpc:.` with `--go_out=` and `--go-grpc_out=` |
| 2 | I-03 | Flaky istio-ambient tests disabled with no resolution path | High | `.github/workflows/kind-e2e.yaml` | 93–94, 127–132, 151 | Hard | Fix root flakiness tracked in #14637, or remove dead test code entirely |
| 3 | I-18 | Binary downloaded with `curl \| tar` — no checksum verification | High | `.github/workflows/kind-e2e.yaml` | 188 | Medium | Download checksum file separately and verify before extracting `gotestsum` tarball |
| 4 | I-01 | Reusable workflows pinned to `@main` — non-deterministic and insecure | High | `knative-go-test.yaml:17`, `knative-go-build.yaml:14`, `knative-style.yaml:15`, `knative-verify.yaml:26`, `knative-security.yaml:17`, `knative-stale.yaml:14`, `kind-e2e.yaml:31,156` | Multiple | Easy | Pin all `uses:` references to specific commit SHAs or release tags instead of `@main` |
| 5 | I-20 | `containedctx` and `contextcheck` linters disabled — TODOs unresolved | Medium | `.golangci.yaml` | 29–36 | Hard | Enable linters and fix all violations, or document a concrete timeline for doing so |
| 6 | I-26 | Overly broad `actions: write` permission in workflow approve job | Medium | `.github/workflows/pr-gh-workflow-approve.yaml` | 18–19 | Medium | Restrict to job-level minimum required permissions |
| 7 | I-27 | `continue-on-error: true` on critical workflow-approve step | Medium | `.github/workflows/pr-gh-workflow-approve.yaml` | 23 | Medium | Replace with explicit error handling and logging; do not silently swallow failures |
| 8 | I-19 | `sudo` used unnecessarily in CI steps | Medium | `.github/workflows/kind-e2e.yaml` | 60, 190 | Medium | Refactor to avoid `sudo` where not required; `sudo echo` on line 60 is redundant |
| 9 | I-06 | `controller-gen` pinned to `v0.16.5` — likely outdated for Go 1.25 | Medium | `hack/update-schemas.sh` | 29 | Medium | Update to latest `controller-gen` version compatible with Go 1.25 (likely v0.17+) |
| 10 | I-02 | `chainguard-dev/actions` pinned to commit SHAs labelled `# main` | Medium | `.github/workflows/kind-e2e.yaml` | 177, 223 | Medium | Track versioned releases from `chainguard-dev/actions` or formalize the SHA-pinning policy |
| 11 | I-14 | `ko-build/setup-ko` at `v0.9` — v1.x available | Medium | `.github/workflows/kind-e2e.yaml` | 49 | Easy | Update to latest stable `v1.x` release |
| 12 | I-09 | `actions/checkout` at `v6.0.2` — newer versions available | Medium | `.github/workflows/kind-e2e.yaml` | 28, 158 | Easy | Update to latest stable release (v8.x+) |
| 13 | I-10 | `actions/cache` at `v5.0.3` — newer versions available | Medium | `.github/workflows/kind-e2e.yaml` | 38, 162 | Easy | Update to latest stable release (v6.x+) |
| 14 | I-11 | `actions/upload-artifact` at `v7.0.0` — newer versions available | Medium | `.github/workflows/kind-e2e.yaml` | 71 | Easy | Update to latest stable release (v8.x+) |
| 15 | I-12 | `actions/download-artifact` at `v8.0.0` — newer versions available | Medium | `.github/workflows/kind-e2e.yaml` | 171 | Easy | Update to latest stable release (v9.x+) |
| 16 | I-13 | `actions/github-script` at `v8.0.0` — newer versions available | Medium | `.github/workflows/pr-gh-workflow-approve.yaml` | 22 | Easy | Update to latest stable release (v9.x or v10.x) |
| 17 | I-24 | K8s test matrix only covers v1.34 and v1.35 — v1.36+ not tested | Medium | `.github/workflows/kind-e2e.yaml` | 84–86 | Easy | Add latest stable Kubernetes versions to the `k8s-version` matrix |
| 18 | I-21 | Dependabot checks GitHub Actions only monthly | Low | `.github/dependabot.yaml` | 6–8 | Easy | Change `interval` from `monthly` to `weekly` for faster security patch detection |
| 19 | I-22 | Dependabot ignores `chainguard-dev` and `knative/actions` | Low | `.github/dependabot.yaml` | 14–16 | Easy | Remove ignore entries if these publish releases; document rationale if exemption is intentional |
| 20 | I-23 | `GOTESTSUM_VERSION: 1.12.0` hardcoded — no auto-update mechanism | Low | `.github/workflows/kind-e2e.yaml` | 19 | Easy | Add Dependabot or a renovate rule to track `gotestsum` releases |
| 21 | I-08 | `go-spew` pinned to a 2018 pre-release commit | Low | `go.mod` | 8 | Easy | Investigate if a stable release can be used; if the pre-release is required, add a comment explaining why |
| 22 | I-29 | Protobuf `plugins=grpc` flag deprecated in newer `protoc` | High | `hack/update-codegen.sh` | 47 | Hard | Separate the `--go_out` and `--go-grpc_out` invocations (part of the gogo/protobuf migration) |
| 23 | I-28 | No actionable resolution plan for `istio-ambient` flaky tests | High | `.github/workflows/kind-e2e.yaml` | 93–94 | Hard | Open a dedicated tracking issue with acceptance criteria and assign an owner |

---

## Issues — Serial List

1. **[I-07] gogo/protobuf deprecated (Hard)** — `hack/update-codegen.sh:47`. The `gogo/protobuf` project is unmaintained. The `--gogofaster_out=plugins=grpc:.` flag does not work in modern `protoc`. Migrate to `google/protobuf` with `--go_out=` and `--go-grpc_out=`.

2. **[I-03] Flaky istio-ambient tests silently disabled (Hard)** — `.github/workflows/kind-e2e.yaml:93–94,127–132,151`. Tests have been commented out since issue #14637 with no owner or timeline. Either fix the underlying flakiness or delete the dead code.

3. **[I-18] Unverified binary download via curl pipe (Medium)** — `.github/workflows/kind-e2e.yaml:188`. `gotestsum` is downloaded and extracted without checksum verification, creating a supply-chain risk. Download the `.sha256` file and verify before extraction.

4. **[I-01] Workflows pinned to @main branch (Easy)** — `knative-go-test.yaml:17`, `knative-go-build.yaml:14`, `knative-style.yaml:15`, `knative-verify.yaml:26`, `knative-security.yaml:17`, `knative-stale.yaml:14`, `kind-e2e.yaml:31,156`. Using `@main` makes CI non-deterministic and vulnerable to upstream changes. Pin all `uses:` to specific commit SHAs or version tags.

5. **[I-20] Disabled linters with unresolved TODOs (Hard)** — `.golangci.yaml:29–36`. `containedctx` and `contextcheck` are disabled pending follow-up PRs that never landed. Enable them and fix violations, or set a concrete deadline.

6. **[I-26] Overly broad `actions: write` permission (Medium)** — `.github/workflows/pr-gh-workflow-approve.yaml:18–19`. The `actions: write` permission is granted at the workflow level. Restrict it to the minimum required scope at the job level.

7. **[I-27] `continue-on-error: true` on critical approve step (Medium)** — `.github/workflows/pr-gh-workflow-approve.yaml:23`. Silent failures on the approval step can leave PRs blocked without any visible signal. Add structured error handling and alerting.

8. **[I-19] Unnecessary `sudo` usage in CI (Medium)** — `.github/workflows/kind-e2e.yaml:60,190`. `sudo echo` on line 60 is a no-op and a bad practice. Refactor both lines to avoid `sudo` where root access is not genuinely required.

9. **[I-06] `controller-gen` pinned to potentially outdated v0.16.5 (Medium)** — `hack/update-schemas.sh:29`. Go 1.25 may require a newer `controller-gen`. Update to the latest version compatible with the current toolchain and pin it in `tools.go`.

10. **[I-02] chainguard-dev actions pinned to `# main` SHAs (Medium)** — `.github/workflows/kind-e2e.yaml:177,223`. SHAs are present but the `# main` comments indicate they track a moving target. Formalize a versioned release policy or document the SHA-pinning discipline.

11. **[I-14] `ko-build/setup-ko` at v0.9 (Easy)** — `.github/workflows/kind-e2e.yaml:49`. v1.x is available with Node.js and performance improvements. Update to the latest stable release.

12. **[I-09] `actions/checkout` at v6.0.2 (Easy)** — `.github/workflows/kind-e2e.yaml:28,158`. Update to v8.x+ for latest security patches and performance improvements.

13. **[I-10] `actions/cache` at v5.0.3 (Easy)** — `.github/workflows/kind-e2e.yaml:38,162`. Update to v6.x+ for improved reliability and bug fixes.

14. **[I-11] `actions/upload-artifact` at v7.0.0 (Easy)** — `.github/workflows/kind-e2e.yaml:71`. Update to v8.x+ for latest improvements.

15. **[I-12] `actions/download-artifact` at v8.0.0 (Easy)** — `.github/workflows/kind-e2e.yaml:171`. Update to v9.x+ for latest improvements.

16. **[I-13] `actions/github-script` at v8.0.0 (Easy)** — `.github/workflows/pr-gh-workflow-approve.yaml:22`. Update to v9.x or v10.x for latest security patches.

17. **[I-24] K8s test matrix missing v1.36+ (Easy)** — `.github/workflows/kind-e2e.yaml:84–86`. Only v1.34 and v1.35 are tested. Add the latest stable Kubernetes releases to catch regressions early.

18. **[I-21] Dependabot on monthly schedule for Actions (Easy)** — `.github/dependabot.yaml:6–8`. Monthly checks are too infrequent for security-critical action updates. Change to `weekly`.

19. **[I-22] Dependabot ignores chainguard-dev and knative/actions (Easy)** — `.github/dependabot.yaml:14–16`. These packages are explicitly exempted from Dependabot, meaning security PRs will never be auto-proposed. Remove the ignore list or document the security rationale.

20. **[I-23] `GOTESTSUM_VERSION` hardcoded with no auto-update (Easy)** — `.github/workflows/kind-e2e.yaml:19`. Add a Dependabot or Renovate rule to track `gotestsum` releases automatically.

21. **[I-08] `go-spew` pinned to a 2018 pre-release commit (Easy)** — `go.mod:8`. `github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc` is nearly 8 years old. Investigate if a stable published release is compatible; if the pre-release is required, add an explanatory comment.

22. **[I-29] Deprecated `plugins=grpc` flag in protoc (Hard)** — `hack/update-codegen.sh:47`. This is a subset of the `gogo/protobuf` migration (I-07). The `plugins` parameter was removed in newer `protoc-gen-go`. Separate into distinct `--go_out` and `--go-grpc_out` invocations.

23. **[I-28] No owner or plan for `istio-ambient` test resolution (Hard)** — `.github/workflows/kind-e2e.yaml:93–94`. The disabled tests from issue #14637 have no assigned owner. Open a dedicated tracking issue with acceptance criteria and assign responsibility.

---

## Difficulty Summary

| Difficulty | Count | Issue IDs |
|------------|-------|-----------|
| Easy | 13 | I-01, I-08, I-09, I-10, I-11, I-12, I-13, I-14, I-21, I-22, I-23, I-24 |
| Medium | 6 | I-02, I-06, I-18, I-19, I-26, I-27 |
| Hard | 4 | I-03, I-07, I-20, I-28, I-29 |

> **Total: 23 issues** (7 removed — PRs raised for I-04, I-15, I-16, I-25, I-30)
