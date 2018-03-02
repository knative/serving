def _ca_bundle_impl(ctx):
  ctx.symlink(Label("//:BUILD.ca_bundle"), "BUILD")
  result = ctx.execute([
      "kubectl", "get", "configmap",
      "-n", "kube-system",
      "extension-apiserver-authentication",
      "-o=jsonpath={.data.client-ca-file}"])
  
  if result.return_code != 0:
    fail("Failed to get ca bundle: %s" % result.stderr)
    
  ctx.file("client-ca-file", content=result.stdout)

ca_bundle = repository_rule(
    implementation = _ca_bundle_impl,
)
