#### Apply the half-balanced mig-parted config
```console
sudo -E nvidia-mig-parted apply -f mig-parted-config.yaml -c half-balanced
```

#### Show the current MIG configuration of the machine
```console
nvidia-smi -L
```

#### Show current state of the cluster
```console
kubectl get pod -A
```

#### Deploy the 4 example apps discussed in the slides
```console
kubectl apply --filename=gpu-test{1,2,3,4}.yaml
```

#### Show all the pods starting up
```console
kubectl get pod -A
```

#### Show the yaml files for the first 3 example apps
```console
vim -O gpu-test1.yaml gpu-test2.yaml gpu-test3.yaml
```

#### Show the GPUs allocated to each
```console
kubectl logs -n gpu-test1 -l app=pod
kubectl logs -n gpu-test2 pod --all-containers
kubectl logs -n gpu-test3 -l app=pod
```

#### Show the yaml file for the complicated example with MIG devices
```console
vim -O gpu-test4.yaml
```

#### Show the pods running
```console
kubectl get pod -A
```

#### Show the output of nvidia-smi
```console
nvidia-smi -L
```

#### Show the MIG devices allocated to each pod
```console
for pod in \
  $(kubectl get pod \
  -n gpu-test4 \
  --output=jsonpath='{.items[*].metadata.name}'); \
do \
  echo "${pod}:"
  kubectl logs -n gpu-test4 ${pod} -c ctr0
  kubectl logs -n gpu-test4 ${pod} -c ctr1
  kubectl logs -n gpu-test4 ${pod} -c ctr2
  kubectl logs -n gpu-test4 ${pod} -c ctr3
  echo ""
done
```

#### Delete this example
```console
kubectl delete -f gpu-test4.yaml
```

#### Show the pods terminating
```console
kubectl get pod -A
```

#### Show the output of nvidia-smi
```console
nvidia-smi -L
```
