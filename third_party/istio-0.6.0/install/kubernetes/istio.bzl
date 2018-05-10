def _subst_ca_bundle_impl(ctx):
  ctx.actions.expand_template(
      template=ctx.file.template,
      output=ctx.outputs.out,
      substitutions={
          "${CA_BUNDLE}": ctx.attr.ca_bundle,
      })

subst_ca_bundle = rule(
    implementation=_subst_ca_bundle_impl,
    attrs={
        "template": attr.label(allow_files=True, single_file=True),
        "ca_bundle": attr.string(mandatory=True),
    },
    outputs={"out": "%{name}.yaml"}
)
