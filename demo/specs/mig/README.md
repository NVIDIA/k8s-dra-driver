#### Show the current MIG configuration of the machine
```console
nvidia-smi --query-gpu=index,name,uuid,mig.mode.current --format=csv
nvidia-smi -L
```

#### Show current state of the cluster
```console
kubectl get pod -A
```

#### Show the yaml files for MIG example apps:
```console
vim -O gpu-test4.yaml gpu-test5.yaml gpu-test6.yaml
```

#### Deploy the 3 MIG example apps above: 
```console
kubectl apply --filename=gpu-test{4,5,6}.yaml
```

#### Show all the pods starting up:
```console
kubectl get pod -A -l app=pod
```

#### Show the output of nvidia-smi:
```console
nvidia-smi -L
```

#### Show the MIG devices allocated to each pod in gpu-test4
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

#### Delete this MIG examples:
```console
kubectl delete --filename=gpu-test{4,5,6}.yaml
```

#### Show the pods terminating:
```console
kubectl get pods -A -l app=pod
```

#### Show the output of nvidia-smi
```console
nvidia-smi -L
```
