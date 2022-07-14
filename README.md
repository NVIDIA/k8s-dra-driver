# The NVIDIA Kubernetes Dynamic Resource Driver

This driver is currently under active development and not yet designed for use.
We will continually be force pushing over `main` until we have something more stable.
Use at your own risk.

### Prerequisites:
1. A working kubernetes cluster based off https://github.com/kubernetes/kubernetes/pull/111023
2. At least one node in the cluster with a working NVIDIA driver and GPU
3. A CDI compliant low-level runtime running on those nodes (e.g. the latest development releases of cri-o or containerd)
4. A working docker installation (to build the container images)

### Install the nvidia.yaml CDI file
```
sudo mkdir -p /etc/cdi
sudo cp ./nvidia.yaml /etc/cdi
```
Customize as necessary for your GPU driver version

### Set the following label on your GPU nodes where you want the node-plugin to run
```
kubectl label node <node-name> --overwrite nvidia.com/dra.plugin=true
kubectl label node <node-name> --overwrite nvidia.com/dra.controller=true
```

### Build the driver components
```
sudo ./build-devel.sh
```

### Deploy the driver components
```
kubectl apply -f deployments/static/crds; kubectl apply -f deployments/static/driver.yaml
```

### Run the examples
```
kubectl apply -f deployments/static/<example>.yaml
```
