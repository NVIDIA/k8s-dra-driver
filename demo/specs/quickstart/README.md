#### Show current state of the cluster
```console
kubectl get pod -A
```

#### Show the yaml files for the first 3 example apps discussed in the [KubeCon presentation](https://sched.co/1R2oG)
```console
vim -O gpu-test1.yaml gpu-test2.yaml gpu-test3.yaml
```

#### Deploy the 3 example apps above 
```console
kubectl apply --filename=gpu-test{1,2,3}.yaml
```

#### Show all the pods starting up
```console
kubectl get pod -A
```

#### Show the GPUs allocated to each
```console
kubectl logs -n gpu-test1 -l app=pod
kubectl logs -n gpu-test2 pod --all-containers
kubectl logs -n gpu-test3 -l app=pod
```

#### Show the MPS (Multi-Process Service) example

```console
vim -O gpu-test-mps.yaml
```

#### Deploy the MPS example
```console
kubectl apply -f gpu-test-mps.yaml
```

#### Show the pod running
```console
kubectl get pod -n sharing-demo -l app=pod
```
