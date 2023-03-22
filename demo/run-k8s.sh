#!/usr/bin/env bash
cd ../../kubernetes

# Reset logs and k8s state
sudo rm -rf /tmp/kube*
sudo rm -rf /var/run/kubernetes

# Setup a custom resolv.conf file
sudo cat > /etc/resolv-9999.conf <<EOF
nameserver 8.8.8.8
EOF

# Export variables needed to run k8s with DRA enabled
export RUNTIME_CONFIG="resource.k8s.io/v1alpha2"
export FEATURE_GATES=DynamicResourceAllocation=true 
export KUBELET_RESOLV_CONF="/etc/resolv-9999.conf"
export DNS_ADDON="coredns"
export CGROUP_DRIVER=systemd
export CONTAINER_RUNTIME_ENDPOINT=unix:///var/run/containerd/containerd.sock
export LOG_LEVEL=6
export ENABLE_CSI_SNAPSHOTTER=false
export API_SECURE_PORT=6444
export ALLOW_PRIVILEGED=1
export PATH=$(pwd)/third_party/etcd:$PATH

# Start bringing up the cluster in the background
exec 3< <(./hack/local-up-cluster.sh -O 2>&1)

# Setup KUBECONFIG, wait until the cluster is up, and install calico
export KUBECONFIG=/var/run/kubernetes/admin.kubeconfig
run-one-until-success kubectl get node 127.0.0.1 
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.24.0/manifests/calico.yaml

# Dump the output of bringing up the cluster and wait to be terminated
cat /dev/fd/3
