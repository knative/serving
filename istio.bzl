def _disable_policy_impl(ctx):
  ctx.actions.expand_template(
      template=ctx.file.template,
      output=ctx.outputs.out,
      substitutions={
          "policy: enabled": "policy: disabled",
      })

disable_policy = rule(
    implementation=_disable_policy_impl,
    attrs={
        "template": attr.label(allow_files=True, single_file=True),
    },
    outputs={"out": "%{name}.yaml"}
)
