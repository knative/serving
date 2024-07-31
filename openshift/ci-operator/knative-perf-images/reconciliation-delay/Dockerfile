# Do not edit! This file was generated via Makefile
FROM registry.ci.openshift.org/openshift/release:rhel-8-release-golang-1.22-openshift-4.17 as builder

COPY . .

RUN ./openshift/in-docker-patch.sh
RUN make perf-install

FROM registry.access.redhat.com/ubi8/ubi-minimal
USER 65532

COPY --from=builder /go/bin/reconciliation-delay /ko-app/reconciliation-delay
ENTRYPOINT ["/ko-app/reconciliation-delay"]
