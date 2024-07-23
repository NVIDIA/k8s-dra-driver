# Basic examples for a Linux desktop or workstation
* [Prerequsites](#prerequsites)
 * [Examples with different DRA configuration](#examples-with-different-dra-configurations)
     * [1. A single pod accesses a GPU via ResourceClaimTemplate](#example-1-spsc-gpu-a-single-pod-accesses-a-gpu-via-resourceclaimtemplate)
     * [2. A single pod's multiple containers share a GPU via ResourceClaimTemplate](#example-2-spmc-shared-gpu-a-single-pods-multiple-containers-share-a-gpu-via-resourceclaimtemplate)
     * [3. Multiple pods share a GPU via ResourceClaim](#example-3-mpsc-shared-gpu-multiple-pods-share-a-gpu-via-resourceclaim)
     * [4. Multiple pods request dedicated GPU access](#example-4-mpsc-unshared-gpu-multiple-pods-request-dedicated-gpu-access)
     * [5. A single pod's multiple containers share a GPU via MPS](#example-5-spmc-mps-gpu-a-single-pods-multiple-containers-share-a-gpu-via-mps)
     * [6. Multiple pods share a GPU via MPS](#example-6-mpsc-mps-gpu-multiple-pods-share-a-gpu-via-mps)
     * [7. A singile pod's multiple containers share a GPU via TimeSlicing](#example-7-spmc-timeslicing-gpu-a-single-pods-multiple-containers-share-a-gpu-via-timeslicing)
     * [8. Multiple pods share a GPU via TimeSlicing](#example-8-mpsc-timeslicing-gpu-multiple-pods-share-a-gpu-via-timeslicing)

## Prerequsites

You will need a Linux machine with a NVIDIA GPU such as GeForce, install the DRA driver and create a kind cluster by following the instructions in the [DRA driver setup](https://github.com/yuanchen8911/k8s-dra-driver?tab=readme-ov-file#demo).

#### Show the current GPU configuration of the machine
```console
nvidia-smi -L
```

```
GPU 0: NVIDIA GeForce RTX 4090 (UUID: GPU-84f293a6-d610-e3dc-c4d8-c5d94409764b)
```

#### Show the cluster up
```console
kubectl cluster-info
kubectl get nodes
```

```
Kubernetes control plane is running at https://127.0.0.1:34883
CoreDNS is running at https://127.0.0.1:34883/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy

To further debug and diagnose cluster problems, use 'kubectl cluster-info dump'.

NAME                                   STATUS   ROLES           AGE    VERSION
k8s-dra-driver-cluster-control-plane   Ready    control-plane   4d1h   v1.29.1
k8s-dra-driver-cluster-worker          Ready    <none>          4d1h   v1.29.1
```

#### Show the DRA-driver running
```console
kubectl get pod -n nvidia-dra-driver
```

```
NAME                                                READY   STATUS    RESTARTS   AGE
nvidia-k8s-dra-driver-controller-6d5869d478-rr488   1/1     Running   0          4d1h
nvidia-k8s-dra-driver-kubelet-plugin-qqq5b          1/1     Running   0          4d1h
```


## Examples with different DRA configurations

#### Example 1 (SPSC-GPU): a single pod accesses a GPU via ResourceClaimTemplate

```console
kubectl apply -f single-pod-single-container-gpu.yaml
sleep 2
kubectl get pods -n spsc-gpu-test
```

The pod will be running.
```
NAME      READY   STATUS    RESTARTS   AGE
gpu-pod   1/1     Running   0          6s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```

```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A   1474787      C   /cuda-samples/sample                        746MiB |
+---------------------------------------------------------------------------------------+
```

Delete the pod:
```console
kubectl delete -f single-pod-single-container-gpu.yaml
```

#### Example 2 (SPMC-Shared-GPU): a single pod's multiple containers share a GPU via ResourceClaimTemplate

```console
kubectl apply -f  single-pod-multiple-containers-shared-gpu.yaml
sleep 2
kubectl get pods -n spmc-shared-gpu-test
```

The pod will be running.
```
NAME      READY   STATUS    RESTARTS      AGE
gpu-pod   2/2     Running   2 (55s ago)   2m13s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A   1514114      C   /cuda-samples/sample                        746MiB |
|    0   N/A  N/A   1514167      C   /cuda-samples/sample                        746MiB |
+---------------------------------------------------------------------------------------+
```

Delete the pod:
```console
kubectl delete -f single-pod-single-container-gpu.yaml
```

#### Example 3 (MPSC-Shared-GPU): multiple pods share a GPU via ResourceClaim

```console
kubectl apply -f multiple-pods-single-container-shared-gpu.yaml
sleep 2
kubectl get pods -n mpsc-shared-gpu-test
```

Two pods will be running.
```
$ kubectl get pods -n mpsc-shared-gpu-test
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod-1   1/1     Running   0          11s
gpu-pod-2   1/1     Running   0          11s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|    0   N/A  N/A   1551456      C   /cuda-samples/sample                    746MiB     |
|    0   N/A  N/A   1551593      C   /cuda-samples/sample                    746MiB     |
|=======================================================================================|
```

Delete the pods:
```console
kubectl delete -f multiple-pods-single-container-shared-gpu.yaml
```

#### Example 4 (MPSC-Unshared-GPU): multiple pods request dedicated GPU access

```console
kubectl apply -f multiple-pods-single-container-unshared-gpu.yaml
sleep 2
kubectl get pods -n mpsc-unshared-gpu-test
```

One pod will be running and the other one is pending.
```
$ kubectl get pods -n mpsc-unshared-gpu-test
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod-1   1/1     Running   0          11s
gpu-pod-2   1/1     Pending   0          11s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|    0   N/A  N/A   1544488      C   /cuda-samples/sample                    746MiB     |
|=======================================================================================|
```

Delete the pods:
```
kubectl delete -f multiple-pods-single-container-unshared-gpu.yaml
```

#### Example 5 (SPMC-MPS-GPU): a single pod's multiple containers share a GPU via MPS

```console
kubectl apply -f single-pod-multiple-containers-mps-gpu.yaml
sleep 2
kubectl get pods -n spmc-mps-gpu-test
```

The pod will be running.
```
$ kubectl get pods -n mpsc-mps-gpu-test
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod-1   2/2     Running   0          11s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A   1559554    M+C   /cuda-samples/sample                     790MiB    |
|    0   N/A  N/A   1559585      C   nvidia-cuda-mps-server                   28MiB     |
|    0   N/A  N/A   1559610    M+C   /cuda-samples/sample                     790MiB    |
+---------------------------------------------------------------------------------------+
```

Delete the pod:
```
kubectl delete -f single-pod-multiple-containers-mps-gpu.yaml
```

#### Example 6 (MPSC-MPS-GPU): multiple pods share a GPU via MPS 

```console
kubectl apply -f multiple-pods-single-container-mps-gpu.yaml
sleep 2
kubectl get pods -n mpsc-mps-gpu-test
```

Two pods will be running and the other one is pending.
```
$ kubectl get pods -n mpsc-mps-gpu-test
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod-1   1/1     Running   0          11s
gpu-pod-2   1/1     Running   0          11s
```

Running `nvidia-smi` will show something like the following:
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A   1568768    M+C   /cuda-samples/sample                        562MiB |
|    0   N/A  N/A   1568771    M+C   /cuda-samples/sample                        562MiB |
|    0   N/A  N/A   1568831      C   nvidia-cuda-mps-server                       28MiB |
+---------------------------------------------------------------------------------------+
```

Delete the pods:
```console
kubectl delete -f multiple-pods-single-container-mps-gpu.yaml
```

#### Example 7 (SPMC-TimeSlicing-GPU): a single pod's multiple containers share a GPU via TimeSlicing

```console
kubectl apply -f single-pod-multiple-containers-timeslicing-gpu.yaml
sleep 2
kubectl get pods -n spmc-timeslicing-gpu-test
```

Two pods will be running and the other one is pending.
```
$ kubectl get pods -n spmc-timeslicing-gpu-test
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod     1/1     Running   0          11s
```

Run `nvidia-smi` will show something like the following (2 containers sharing the GPU):
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A    306436      C   /cuda-samples/sample                        746MiB |
|    0   N/A  N/A    306442      C   ./gpu_burn                                21206MiB |
+---------------------------------------------------------------------------------------+```
```

Delete the pods:
```console
kubectl delete -f single-pod-multiple-containers-timeslicing-gpu.yaml
```

#### Example 8 (MPSC-TimeSlicing-GPU): multiple pods share a GPU via TimeSlicing

```console
kubectl apply -f multiple-pods-single-container-timeslicing-gpu.yaml
sleep 2
kubectl get pods -n mpsc-timeslicing-gpu-test
```

Two pods will be running and the other one is pending.
```
$ kubectl get pods -n mpsc-timeslicing-gpu-test 
NAME        READY   STATUS    RESTARTS   AGE
gpu-pod-1     1/1     Running   0          11s
gpu-pod-2     1/1     Running   0          11s
```

Run `nvidia-smi` will show something like the following (2 containers sharing the GPU):
```console
nvidia-smi
```
```
+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
|=======================================================================================|
|    0   N/A  N/A    306436      C   /cuda-samples/sample                        746MiB |
|    0   N/A  N/A    306442      C   ./gpu_burn                                21206MiB |
+---------------------------------------------------------------------------------------+```
```

Delete the pods:
```console
kubectl delete -f multiple-pods-single-containers-timeslicing-gpu.yaml
```
