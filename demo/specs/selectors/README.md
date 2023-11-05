#### List the set of nodes in the cluster
```console
kubectl get nodes -A
```

#### Show the set of nodes which have GPUs available
```console
kubectl get nodeallocationstates.nas.gpu.resource.nvidia.com -A
```

#### Show the set of allocatable GPUs from each node
```console
kubectl get nodeallocationstates.nas.gpu.resource.nvidia.com -A -o=json \
	| jq -r '.items[] 
             | "\(.metadata.name):",
             (.spec.allocatableDevices[])'
```

### Open the yaml files with the specs for the demo
```console
vi -O parameters.yaml claims.yaml pods.yaml
```

#### Create a namespace for the demo and deploy the demo pods
```console
kubectl create namespace kubecon-demo
kubectl apply -f parameters.yaml -f claims.yaml -f pods.yaml
```

#### Show the pods running
```console
kubectl get pod -n kubecon-demo
```

#### Show the set of GPUs allocated to some claim
```console
kubectl get nodeallocationstates.nas.gpu.resource.nvidia.com -A -o=json \
	| jq -r '.items[]
             | select(.spec.allocatedClaims)
             | "\(.metadata.name):",
             (.spec.allocatedClaims[])'
```

#### Show the logs of the inference and training pods
```console
kubectl logs -n kubecon-demo inference-pod
kubectl logs -n kubecon-demo training-pod
```
