# Running the NVIDIA DRA Driver on Red Hat OpenShift

This document explains the differences between deploying the NVIDIA DRA driver on OpenShift and upstream Kubernetes or its flavors.

## Prerequisites

Install a recent build of OpenShift 4.16 (e.g. 4.16.0-ec.4). You can use the Assisted Installer to install on bare metal, or obtain an IPI installer binary (`openshift-install`) from the [Release Status](https://amd64.ocp.releases.ci.openshift.org/) page. Note that a development version of OpenShift requires access to [an internal CI registry](https://docs.ci.openshift.org/docs/how-tos/use-registries-in-build-farm/) in the pull secret. Refer to the [OpenShift documentation](https://docs.openshift.com/container-platform/4.15/installing/index.html) for different installation methods.

## Enabling DRA on OpenShift

Enable the `TechPreviewNoUpgrade` feature set as explained in [Enabling features using FeatureGates](https://docs.openshift.com/container-platform/4.15/nodes/clusters/nodes-cluster-enabling-features.html), either during the installation or post-install. The feature set includes the `DynamicResourceAllocation` feature gate.

Update the cluster scheduler to enable the DRA scheduling plugin:

```console
$ oc patch --type merge -p '{"spec":{"profile": "HighNodeUtilization", "profileCustomizations": {"dynamicResourceAllocation": "Enabled"}}}' scheduler cluster
```

## NVIDIA GPU Drivers

The easiest way to install NVIDIA GPU drivers on OpenShift nodes is via the NVIDIA GPU Operator.

**Be careful to disable the device plugin so it does not conflict with the DRA plugin**:

```yaml
  devicePlugin:
    enabled: false
```

Keep in mind that the NVIDIA GPU operator is needed here only to install NVIDIA binaries on the cluster nodes.

The operator might not be available through the OperatorHub in a pre-production version of OpenShift. In this case, deploy the operator from a bundle or add a certified catalog index from an earlier version of OpenShift, e.g.:

```yaml
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
```

Then follow the installation steps in [NVIDIA GPU Operator on Red Hat OpenShift Container Platform](https://docs.nvidia.com/datacenter/cloud-native/openshift/latest/index.html).

## NVIDIA Binaries on RHCOS

The location of some NVIDIA binaries on an OpenShift node differs from the defaults. Make sure to pass the following values when installing the Helm chart:

```yaml
nvidiaDriverRoot: /run/nvidia/driver
nvidiaCtkPath: /var/usrlocal/nvidia/toolkit/nvidia-ctk
```

## OpenShift Security

OpenShift generally requires more stringent security settings than Kubernetes. If you see a warning about security context constraints when deploying the DRA plugin, pass the following to the Helm chart, either via an in-line variable or a values file:

```yaml
kubeletPlugin:
  containers:
    plugin:
      securityContext:
        privileged: true
        seccompProfile:
          type: Unconfined
```

If you see security context constraints errors/warnings when deploying a sample workload, make sure to update the workload's security settings according to the [OpenShift documentation](https://docs.openshift.com/container-platform/4.15/operators/operator_sdk/osdk-complying-with-psa.html). Usually applying the following `securityContext` definition at a pod or container level works for non-privileged workloads.

```yaml
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
```

## Using Multi-Instance GPU (MIG)

Workloads that use the Multi-instance GPU (MIG) feature require MIG to be enabled on the worker nodes with [MIG-supported GPUs](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html#supported-gpus), e.g. A100.

First, make sure to stop any custom pods that might be using the GPU. Disable the DCGM and DCGM Exporter of the NVIDIA GPU Operator by editing the operator's cluster policy (set `enabled: false`).

Enable MIG via the MIG manager of the NVIDIA GPU Operator. **Do not configure MIG devices as the DRA driver will do it automatically on the fly.**

1. In the GPU operator namespace, create a `ConfigMap` with the key `config.yaml` and the following content:

  ```yaml
  version: v1
  mig-configs:
    all-enabled:
      - devices: all
        mig-enabled: true
        mig-devices: {}
    all-disabled:
     - devices: all
       mig-enabled: false
  ```

2. Update the cluster policy to point the MIG manager to the new `ConfigMap`:

  ```yaml
  migManager:
    config:
      name: <configmap_name>
  ```

3. Label the target nodes with `nvidia.com/mig.config=all-enabled`:

  ```console
  $ oc label node <node> nvidia.com/mig.config=all-enabled --overwrite
  ```

MIG will be automatically enabled on the labeled nodes. For additional information, see [MIG Support in OpenShift Container Platform](https://docs.nvidia.com/datacenter/cloud-native/openshift/latest/mig-ocp.html).

You can verify the MIG status using the `nvidia-smi` command from a GPU driver pod:

```console
$ oc exec -ti nvidia-driver-daemonset-<suffix> -n nvidia-gpu-operator -- nvidia-smi
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 550.54.14              Driver Version: 550.54.14      CUDA Version: N/A      |
|-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA A100 80GB PCIe          On  |   00000000:17:00.0 Off |                   On |
| N/A   35C    P0             45W /  300W |       0MiB /  81920MiB |     N/A      Default |
|                                         |                        |              Enabled |
+-----------------------------------------+------------------------+----------------------+
```

If the MIG status is marked with an asterisk (i.e. `Enabled*`), it means that the setting could not be fully applied and you may need to reboot the node.
This can happen on some cloud service providers (CSP) where the CSP blocks GPU reset for the GPUs passed into a VM.

See the [NVIDIA Multi-Instance GPU User Guide](https://docs.nvidia.com/datacenter/tesla/mig-user-guide/index.html) for more information about MIG.
