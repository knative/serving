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

# Add a PreStop sleep so that the sidecar proxy stays up a little bit
# longer (20 seconds) when Pod termination happens.  Changing the
# ConfigMap to customize Istio sidecar injection is something Istio
# expect, as their documentations show examples of doing so.
#
# This substitution here may be a bit ugly, but is is little bit
# better than checking in a modified YAML, in which case we will need
# to manually update the YAML everytime it changes.
def _add_prestop_sleep_impl(ctx):
  ctx.actions.expand_template(
      template=ctx.file.template,
      output=ctx.outputs.out,
      substitutions={
          "      - name: istio-proxy": """      - name: istio-proxy
        lifecycle:
          preStop:
            exec:
              command:
              - /bin/sleep
              - \"20\""""
      })

add_prestop_sleep = rule(
    implementation=_add_prestop_sleep_impl,
    attrs={
        "template": attr.label(allow_files=True, single_file=True),
    },
    outputs={"out": "%{name}.yaml"}
)

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
