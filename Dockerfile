# When bumping the Go version, don't forget to update the configuration of the
# CI jobs in openshift/release.
FROM quay-proxy.ci.openshift.org/openshift/ci:ocp_builder_rhel-9-golang-1.24-openshift-4.20 AS builder
WORKDIR /go/src/github.com/openshift/cluster-baremetal-operator
COPY . .
RUN make build

FROM quay-proxy.ci.openshift.org/openshift/ci:ocp_4.20_base-rhel9
COPY --from=builder /go/src/github.com/openshift/cluster-baremetal-operator/bin/cluster-baremetal-operator /usr/bin/cluster-baremetal-operator
COPY --from=builder /go/src/github.com/openshift/cluster-baremetal-operator/manifests /manifests
LABEL io.openshift.release.operator=true
ENTRYPOINT ["/usr/bin/cluster-baremetal-operator"]
