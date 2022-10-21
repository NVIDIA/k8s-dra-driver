# The NVIDIA GPU Plugin for Dynamic Resource Allocation (DRA) in Kubernetes

This DRA plugin is currently under active development and not yet designed for production use.
We will continually be force pushing over `main` until we have something more stable.
Use at your own risk.

A document and demo of the DRA support for GPUs provided by this repo can be found below:
|                                                                                                                          Document                                                                                                                          |                                                                                                                                                                   Demo                                                                                                                                                                   |
|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|
| [<img width="300" alt="Dynamic Resource Allocation (DRA) for GPUs in Kubernetes" src="https://drive.google.com/uc?export=download&id=12EwdvHHI92FucRO2tuIqLR33OC8MwCQK">](https://docs.google.com/document/d/1BNWqgx_SmZDi-va_V31v3DnuVwYnF2EmN7D-O_fB6Oo) | [<img width="300" alt="Demo of Dynamic Resource Allocation (DRA) for GPUs in Kubernetes" src="https://drive.google.com/uc?export=download&id=1UzB-EBEVwUTRF7R0YXbGe9hvTjuKaBlm">](https://drive.google.com/file/d/1iLg2FEAEilb1dcI27TnB19VYtbcvgKhS/view?usp=sharing "Demo of Dynamic Resource Allocation (DRA) for GPUs in Kubernetes") |

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
Customize as necessary for your GPU and GPU driver version

### Set the following label on your GPU nodes where you want the DRA driver to run
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
