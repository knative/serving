# Generate a thumbnailer.yaml based on the DOCKER_REPO_OVERRIDE that folks set.
genrule(
    name = "gen-with-repo",
    srcs = ["thumbnailer.yaml.tpl"],
    outs = ["thumbnailer.yaml"],
    cmd = """
DOCKER_REPO_OVERRIDE=$$(cat bazel-out/stable-status.txt | grep STABLE_DOCKER_REPO | cut -d' ' -f 2)
    sed "s@DOCKER_REPO_OVERRIDE@$${DOCKER_REPO_OVERRIDE}@g" < $(location thumbnailer.yaml.tpl) > $(location thumbnailer.yaml)
""",
    stamp = 1,
)
