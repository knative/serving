def _cluster_ca_bundle_impl(ctx):
  ctx.symlink(Label("//:BUILD.ca_bundle"), "BUILD")
  cluster = ctx.execute([
      "sh", "-c",
      "grep STABLE_K8S_CLUSTER bazel-out/stable-status.txt | cut -d' ' -f 2"]).stdout

  result = ctx.execute([
      "sh",
      "-c",
      "kubectl get configmap --namespace=kube-system extension-apiserver-authentication -o=jsonpath={.data.client-ca-file} --cluster=%s | base64 | tr -d '\n'" % cluster
  ])
  
  if result.return_code != 0:
    fail("Failed to get ca bundle: %s" % result.stderr)

  ctx.file("BUILD", "exports_files(['bundle.bzl'])")
  ctx.file("bundle.bzl", "CA_BUNDLE='''%s'''" % result.stdout)

cluster_ca_bundle = repository_rule(
    implementation = _cluster_ca_bundle_impl,
)
