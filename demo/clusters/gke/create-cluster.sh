#!/bin/bash

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

: ${PROJECT_NAME:=$(gcloud config list --format 'value(core.project)' 2>/dev/null)}

if [[ -z ${PROJECT_NAME} ]]; then
	echo "Project name could not be determined"
	echo "Please run 'gcloud config set project'"
	exit 1
fi

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
PROJECT_DIR="$(cd -- "$( dirname -- "${CURRENT_DIR}/../../../.." )" &> /dev/null && pwd)"

# We extract information from versions.mk
function from_versions_mk() {
    local makevar=$1
    local value=$(grep -E "^\s*${makevar}\s+[\?:]= " ${PROJECT_DIR}/versions.mk)
    echo ${value##*= }
}
DRIVER_NAME=$(from_versions_mk "DRIVER_NAME")

NETWORK_NAME="${DRIVER_NAME}-net"
CLUSTER_NAME="${DRIVER_NAME}-cluster"

## Create the Network for the cluster
gcloud compute networks create "${NETWORK_NAME}" \
	--quiet \
	--project="${PROJECT_NAME}" \
	--description=Manually\ created\ network\ for\ TMS\ DRA\ Alpha\ cluster \
	--subnet-mode=auto \
	--mtu=1460 \
	--bgp-routing-mode=regional

## Create the cluster
gcloud container clusters create "${CLUSTER_NAME}" \
	--quiet \
	--enable-kubernetes-alpha \
	--no-enable-autorepair \
	--no-enable-autoupgrade \
	--region us-west1 \
	--network "${NETWORK_NAME}" \
	--node-labels=nvidia.com/dra.controller=true

# Create t4 node pool
gcloud beta container node-pools create "pool-1" \
	--quiet \
	--project "${PROJECT_NAME}" \
	--cluster "${CLUSTER_NAME}" \
	--region "us-west1" \
	--node-version "1.27.3-gke.100" \
	--machine-type "n1-standard-8" \
	--accelerator "type=nvidia-tesla-t4,count=1" \
	--image-type "UBUNTU_CONTAINERD" \
	--disk-type "pd-standard" \
	--disk-size "100" \
	--metadata disable-legacy-endpoints=true \
	--scopes "https://www.googleapis.com/auth/devstorage.read_only","https://www.googleapis.com/auth/logging.write","https://www.googleapis.com/auth/monitoring","https://www.googleapis.com/auth/servicecontrol","https://www.googleapis.com/auth/service.management.readonly","https://www.googleapis.com/auth/trace.append" \
	--num-nodes "2" \
	--enable-autoscaling \
	--min-nodes "2" \
	--max-nodes "6" \
	--location-policy "ANY" \
	--no-enable-autoupgrade \
	--no-enable-autorepair \
	--max-surge-upgrade 1 \
	--max-unavailable-upgrade 0 \
	--node-locations "us-west1-a" \
	--node-labels=gke-no-default-nvidia-gpu-device-plugin=true,nvidia.com/gpu=present,nvidia.com/dra.kubelet-plugin=true

# Create v100 node pool
gcloud beta container node-pools create "pool-2" \
	--quiet \
    --project "${PROJECT_NAME}" \
	--cluster "${CLUSTER_NAME}" \
	--region "us-west1" \
	--node-version "1.27.3-gke.100" \
	--machine-type "n1-standard-8" \
	--accelerator "type=nvidia-tesla-v100,count=1" \
	--image-type "UBUNTU_CONTAINERD" \
	--disk-type "pd-standard" \
	--disk-size "100" \
	--metadata disable-legacy-endpoints=true \
	--scopes "https://www.googleapis.com/auth/devstorage.read_only","https://www.googleapis.com/auth/logging.write","https://www.googleapis.com/auth/monitoring","https://www.googleapis.com/auth/servicecontrol","https://www.googleapis.com/auth/service.management.readonly","https://www.googleapis.com/auth/trace.append" \
	--num-nodes "1" \
	--enable-autoscaling \
	--min-nodes "1" \
	--max-nodes "6" \
	--location-policy "ANY" \
	--no-enable-autoupgrade \
	--no-enable-autorepair \
	--max-surge-upgrade 1 \
	--max-unavailable-upgrade 0 \
	--node-locations "us-west1-a" \
	--node-labels=gke-no-default-nvidia-gpu-device-plugin=true,nvidia.com/gpu=present,nvidia.com/dra.kubelet-plugin=true

## Allow the GPU nodes access to the internet
gcloud compute routers create ${NETWORK_NAME}-nat-router \
	--quiet \
	--project "${PROJECT_NAME}" \
	--network "${NETWORK_NAME}" \
	--region "us-west1"

gcloud compute routers nats create "${NETWORK_NAME}-nat-config" \
	--quiet \
	--project "${PROJECT_NAME}" \
    --router "${NETWORK_NAME}-nat-router" \
    --nat-all-subnet-ip-ranges \
    --auto-allocate-nat-external-ips \
    --router-region "us-west1"

## Start using this cluster for kubectl
gcloud container clusters get-credentials "${CLUSTER_NAME}" --location="us-west1"

## Launch the nvidia-driver-installer daemonset to install the GPU drivers on any GPU nodes that come online:
kubectl apply -f https://raw.githubusercontent.com/GoogleCloudPlatform/container-engine-accelerators/master/nvidia-driver-installer/ubuntu/daemonset-preloaded.yaml

## Create the nvidia namespace
kubectl create namespace nvidia

## Deploy a custom daemonset that prepares a node for use with DRA
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-dra-driver/456d097feb452cca1351817bab2ccd0782e96c9f/demo/prepare-gke-nodes-for-dra.yaml
