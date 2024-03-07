#!/usr/bin/env bash

set -ex
set -o pipefail

oc create -f - <<EOF
kind: CatalogSource
apiVersion: operators.coreos.com/v1alpha1
metadata:
  name: certified-operators-v415
  namespace: openshift-marketplace
spec:
  displayName: Certified Operators v4.15
  image: registry.redhat.io/redhat/certified-operator-index:v4.15
  priority: -100
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 10m0s
EOF
