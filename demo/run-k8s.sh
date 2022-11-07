#!/usr/bin/env bash
cd ../../kubernetes

sudo rm -rf /tmp/kube*
sudo rm -rf /var/run/kubernetes

export RUNTIME_CONFIG="resource.k8s.io/v1alpha1"
export FEATURE_GATES=DynamicResourceAllocation=true 
export KUBELET_RESOLV_CONF="/etc/resolv-9999.conf"
export DNS_ADDON="coredns"
export CGROUP_DRIVER=systemd
export CONTAINER_RUNTIME_ENDPOINT=unix:///var/run/containerd/containerd.sock
export LOG_LEVEL=6
export KUBELET_FLAGS="--max-pods=500"
export ENABLE_CSI_SNAPSHOTTER=false
export API_SECURE_PORT=6444
export ALLOW_PRIVILEGED=1
export PATH=$(pwd)/third_party/etcd:$PATH
./hack/local-up-cluster.sh -O
