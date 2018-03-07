def _ca_bundle_impl(ctx):
  ctx.symlink(Label("//:BUILD.ca_bundle"), "BUILD")
  cluster = ctx.execute([
      "sh", "-c",
      "grep STABLE_K8S_CLUSTER bazel-out/stable-status.txt | cut -d' ' -f 2"]).stdout

  result = ctx.execute([
      "kubectl", "get", "configmap",
      "-n", "kube-system",
      "extension-apiserver-authentication",
      "-o=jsonpath={.data.client-ca-file}",
      "--cluster=" + cluster])
  
  if result.return_code != 0:
    fail("Failed to get ca bundle: %s" % result.stderr)
    
  ctx.file("client-ca-file", content=result.stdout)

ca_bundle = repository_rule(
    implementation = _ca_bundle_impl,
)
