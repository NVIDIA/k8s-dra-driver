# Dynamic Resource Allocation (DRA) for NVIDIA GPUs in Kubernetes

This DRA resource driver is currently under active development and not yet
designed for production use.
We will continually be force pushing over `main` until we have something more stable.
Use at your own risk.

A document and demo of the DRA support for GPUs provided by this repo can be found below:
|                                                                                                                          Document                                                                                                                          |                                                                                                                                                                   Demo                                                                                                                                                                   |
|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|
| [<img width="300" alt="Dynamic Resource Allocation (DRA) for GPUs in Kubernetes" src="https://drive.google.com/uc?export=download&id=12EwdvHHI92FucRO2tuIqLR33OC8MwCQK">](https://docs.google.com/document/d/1BNWqgx_SmZDi-va_V31v3DnuVwYnF2EmN7D-O_fB6Oo) | [<img width="300" alt="Demo of Dynamic Resource Allocation (DRA) for GPUs in Kubernetes" src="https://drive.google.com/uc?export=download&id=1UzB-EBEVwUTRF7R0YXbGe9hvTjuKaBlm">](https://drive.google.com/file/d/1iLg2FEAEilb1dcI27TnB19VYtbcvgKhS/view?usp=sharing "Demo of Dynamic Resource Allocation (DRA) for GPUs in Kubernetes") |

### Prerequisites:
1. A `go` installation of `v1.19+`
1. A working kubernetes cluster based off kubernetes `v1.26+`
1. At least one node in the cluster with a working NVIDIA driver and GPU
1. A CDI-enabled container runtime (e.g. cri-o `v1.23.2+` or containerd `v1.7+`)
1. A working docker installation (to build the container images)

### Example deployment

The following example has been tested on a DGX-A100 node running DGX OS 4.99.11 with [`nvidia-mig-parted`](https://github.com/NVIDIA/mig-parted) installed.
All of the steps except for step 6 (and `gpu-test4.yaml` from the `demo` folder) should be relevant to any machine setup though.

The steps involved are:
1. Setup the environment
1. Intall a CDI-enabled containerd and run it
1. Deploy a single-node kubernetes cluster from source
1. Deploy calico as the CNI plugin to use
1. Copy the kubeconfig from this newly created cluster into ~/.kube/config so it will be configured by default
1. Run `nvidia-mig-parted` to pre-configure 4 GPUs on the node into MIG mode and 4 as full GPUs
1. Ensure your NVIDIA driver installation is rooted at `/run/nvidia/driver`
1. Deploy the DRA resource driver and all accompanying CRDs from this repo
1. Point you where to run a set of demo scripts against this resource driver from the `demo` directory

#### Setup the environment
First create a directory to house the various code repos we will use:
```console
mkdir dra-test-env
```

Then `cd` into this directory and clone all of the code repos we will be using throughout this example:
```console
cd dra-test-env
git clone https://github.com/kubernetes/kubernetes.git
git clone https://github.com/containerd/containerd.git
git clone https://gitlab.com/nvidia/cloud-native/k8s-dra-driver.git
```

Build `kubernetes`:
```console
cd kubernetes
git fetch --tags
git checkout v1.26.0
make
cd -
```

Build `containerd`:
```console
sudo apt install libbtrfs-dev -y

cd containerd
git fetch --tags
git checkout v1.7.0
make
cd -
```

#### Install a CDI-enabled `containerd` and run it

First install the standard `containerd.io` package from `apt`
```console
sudo apt update
sudo apt install containerd.io
```

Then `cd` into your checked out `containerd` repo (where you previously built
the source for `v1.7.0`) and install this over the binaries from the
`containerd.io` package you just installed.
```console
cd containerd
sudo PREFIX=/usr make install
cd -
```

**NOTE:** We do it this way to get all of the systemd setup in place using the
packages, but then replace the binaries with the versions that we need.

Next, configure `containerd` to become CDI "aware"
```console
$ sudo bash -c "containerd config default > /etc/containerd/config.toml"
```
```console
$ cat /etc/containerd/config.toml
...
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    cdi_spec_dirs = ["/etc/cdi", "/var/run/cdi"]
...
    enable_cdi = true
...

```

Finally, restart `containerd`:
```console
sudo systemctl restart containerd
```

#### Deploy a single-node kubernetes cluster from source

A script exists in the `demo` folder of this repo to run a single-node kubernetes cluster with DRA enabled.

It assumes three things:
1. You have a `kubernetes` repo checked out at the same level as this `k8s-dra-driver`repo.
1. The `kubernetes` repo is checked out to a versiion `v1.26+`
1. You have already run `make` from this repo.

The first step is to get `etcd` installed and running from the `kubernetes`
repo:
```console
cd kubernetes
./hack/install-etcd.sh
cd -
```

**NOTE:** This only needs to be done **once**, and not everytime you tear down
and bring up the kubernetes cluster as described below.

With this in place, you can now open a new terminal window, `cd` to the `demo` directory of this repo, and run the following script:
```console
cd k8s-dra-driver/demo
sudo ./run-k8s.sh 
```

This script will take care of all steps to:
1. Deploy the single-node kubernetes cluster from source
1. Deploy calico as the CNI plugin to use

Once the cluster is up and running, you can open another terminal and copy the
`kubeconfig` of the cluster into your home directory so that it will be used by
default when running `kubectl`:
```console
cp /var/run/kubernetes/admin.kubeconfig ~/.kube/config
```

Alternatively, you can export the KUBECONFIG environment variable to point to
this `kubeconfig` file, but you must remember to do this in every new terminal
you open, so it's often easier / less error prone to just copy it to
`~/.kube/config`:
```console
export KUBECONFIG=/var/run/kubernetes/admin.kubeconfig
```

Once you have your `kubeconfig` set up you can verify that everything is
running as expected as below:
```console
$ kubectl get pod -A
NAMESPACE     NAME                                       READY   STATUS    RESTARTS   AGE
kube-system   calico-kube-controllers-7cb8bd8f74-wnrqx   1/1     Running   0          28s
kube-system   calico-node-p9sgm                          1/1     Running   0          28s
kube-system   coredns-6d97d5ddb-9f5m7                    1/1     Running   0          38s

```

#### Run `nvidia-mig-parted` to pre-configure 4 GPUs on the node into MIG mode and 4 as full GPUs
This step is only relevant if you are running on a DGX-A100 node with `nvidia-mig-parted` installed.
It can be safely skipped (or modified) for other machine setups.

To configure the GPUs as described, simply run the following script:
```console
cd k8s-dra-driver/demo
sudo ./configure.mig.sh
cd -
```

To then verify the configuration run:
```console
nvidia-smi --query-gpu=index,name,uuid,mig.mode.current --format=csv
```

#### Ensure your NVIDIA driver installation is rooted at `/run/nvidia/driver`
For deployments running a driver container this is a `noop`.
The driver container should already mount the driver installation at `/run/nvidia/driver`.

For deployments running with a host-installed driver, the following is sufficient to meet this requirement:
```console
mkdir -p /run/nvidia
sudo ln -s / /run/nvidia/driver
```

**NOTE:** This is only currently necessary due to a limitation of how our CDI
generation library works. This restriction will be removed very soon.

### Build and deploy the DRA resource driver for GPUs
A set of convenience scripts are provided to build the DRA driver components
into container images and deploy them to the cluster.

Simply run the following command to build them:
```console
cd k8s-dra-driver
sudo ./build-devel.sh
cd -
```

And run the following to deploy them:
```console
cd k8s-dra-driver/demo
./install-dra-driver.sh
cd -
```

You can then verify they are up and running as follows:
```console
$ kubectl get pod -n nvidia-dra-driver
NAMESPACE           NAME                                       READY   STATUS    RESTARTS   AGE
nvidia-dra-driver   nvidia-dra-controller-6bdf8f88cc-psb4r     1/1     Running   0          34s
nvidia-dra-driver   nvidia-dra-plugin-lt7qh                    1/1     Running   0          32s
```

### Run the examples by following the steps in the demo script
Finally, you can run the various examples contained in the `demo` folder.
The files `demo/DEMO.sh` shows the full script of the demo you can walk through.
```console
cat demo/DEMO.sh
...
```

Where the running the first three examples should produce output similar to the following:
```console
$ kubectl apply --filename=gpu-test{1,2,3}.yaml
...

```
```console
$ kubectl get pod -A
NAMESPACE           NAME                                       READY   STATUS    RESTARTS   AGE
gpu-test1           pod1                                       1/1     Running   0          34s
gpu-test1           pod2                                       1/1     Running   0          34s
gpu-test2           pod                                        2/2     Running   0          34s
gpu-test3           pod1                                       1/1     Running   0          34s
gpu-test3           pod2                                       1/1     Running   0          34s
...

```
```console
$ kubectl logs -n gpu-test1 -l app=pod
GPU 0: A100-SXM4-40GB (UUID: GPU-662077db-fa3f-0d8f-9502-21ab0ef058a2)
GPU 0: A100-SXM4-40GB (UUID: GPU-4cf8db2d-06c0-7d70-1a51-e59b25b2c16c)

$ kubectl logs -n gpu-test2 pod --all-containers
GPU 0: A100-SXM4-40GB (UUID: GPU-79a2ba02-a537-ccbf-2965-8e9d90c0bd54)
GPU 0: A100-SXM4-40GB (UUID: GPU-79a2ba02-a537-ccbf-2965-8e9d90c0bd54)

$ kubectl logs -n gpu-test3 -l app=pod
GPU 0: A100-SXM4-40GB (UUID: GPU-4404041a-04cf-1ccf-9e70-f139a9b1e23c)
GPU 0: A100-SXM4-40GB (UUID: GPU-4404041a-04cf-1ccf-9e70-f139a9b1e23c)
```
