#!/bin/bash

LOCALBIN=$(pwd)/bin
mkdir -p ${LOCALBIN}

KUSTOMIZE_VERSION=v5.3.0
CONTROLLER_TOOLS_VERSION=v0.14.0
ENVTEST_VERSION=latest
GOLANGCI_LINT_VERSION=v1.54.2
KUBECTL=kubectl
KUSTOMIZE=${LOCALBIN}/kustomize-${KUSTOMIZE_VERSION}
CONTROLLER_GEN=${LOCALBIN}/controller-gen-${CONTROLLER_TOOLS_VERSION}
ENVTEST=${LOCALBIN}/setup-envtest-${ENVTEST_VERSION}
GOLANGCI_LINT=${LOCALBIN}/golangci-lint-${GOLANGCI_LINT_VERSION}

go-install-tool() {
    [ -f $1 ] || {
    set -e
    package=$2@$3
    echo "Downloading ${package}"
    GOBIN=$1 go install ${package}
    mv $(echo $4 | sed "s/-$3//") $4
    }
}

go-install-tool "${LOCALBIN}" "sigs.k8s.io/kustomize/kustomize/v5" "${KUSTOMIZE_VERSION}" "${KUSTOMIZE}"
go-install-tool "${LOCALBIN}" "sigs.k8s.io/controller-tools/cmd/controller-gen" "${CONTROLLER_TOOLS_VERSION}" "${CONTROLLER_GEN}"

$(echo $CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

cd config/manager && $(echo $KUSTOMIZE) edit set image controller=${IMG} && $(echo $KUSTOMIZE) edit set configmap repo-envvar-configmap-exporter --from-literal=REPO=${REPO}
cd ../..
$(echo $KUSTOMIZE) build config/default | $(echo $KUBECTL) apply -f -
