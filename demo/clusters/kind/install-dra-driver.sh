#!/usr/bin/env bash

# Copyright 2023 The Kubernetes Authors.
# Copyright 2023 NVIDIA CORPORATION.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/scripts/common.sh"

kubectl label node -l node-role.x-k8s.io/worker --overwrite nvidia.com/dra.kubelet-plugin=true
kubectl label node -l node-role.x-k8s.io/control-plane --overwrite nvidia.com/dra.controller=true

helm upgrade -i --create-namespace --namespace nvidia-dra-driver nvidia ${PROJECT_DIR}/deployments/helm/k8s-dra-driver \
    ${NVIDIA_DRIVER_ROOT:+--set nvidiaDriverRoot=${NVIDIA_DRIVER_ROOT}} \
    --wait

kubectl apply -f "${CURRENT_DIR}/scripts/imex-resourceslice.yaml"

set +x
printf '\033[0;32m'
echo "Driver installation complete:"
kubectl get pod -n nvidia-dra-driver
printf '\033[0m'
