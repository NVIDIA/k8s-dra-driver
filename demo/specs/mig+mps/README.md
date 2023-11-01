#### Show the job and its claims
```console
vim -O \
	sharing-demo-job.yaml \
	sharing-demo-claims.yaml \
	sharing-demo-parameters.yaml
```

#### Show current state of the cluster
```console
kubectl get pod -A
```

#### Show the current MIG configuration of the machine
```console
nvidia-smi -L
```

#### Create the demo namespace and deploy the job and its claims
```console
kubectl create namespace sharing-demo
kubectl apply \
	-f sharing-demo-parameters.yaml \
	-f sharing-demo-claims.yaml \
	-f sharing-demo-job.yaml
```

#### Show processes starting to come up
```console
kubectl get pod -A
```

#### Show MIG devices and processes from all job pods executing
```console
nvidia-smi
```

#### Set some environment variables to help us narrow down our view of the running processes
```console
eval "$(./sharing-demo-envs.sh)"
```

#### View the processes for the time-sliced GPU
```console
docker run \
	--rm \
	--pid=host \
	-e NVIDIA_VISIBLE_DEVICES=${GPU_TS_SHARING_DEVICE} \
	ubuntu:22.04 nvidia-smi
```

#### View the processes for the MPS-shared GPU
```console
docker run \
	--rm \
	--pid=host \
	-e NVIDIA_VISIBLE_DEVICES=${GPU_MPS_SHARING_DEVICE} \
	ubuntu:22.04 nvidia-smi
```

#### View the processes for the time-sliced MIG Device
```console
docker run \
	--rm \
	--pid=host \
	-e NVIDIA_VISIBLE_DEVICES=${MIG_TS_SHARING_DEVICE} \
	ubuntu:22.04 nvidia-smi
```

#### View the processes for the MPS-shared MIG Device
```console
docker run \
	--rm \
	--pid=host \
	-e NVIDIA_VISIBLE_DEVICES=${MIG_MPS_SHARING_DEVICE} \
	ubuntu:22.04 nvidia-smi
```

#### Show the running job again
```console
kubectl get pod -A
```

#### Delete the job and its claims
```console
kubectl delete \
	-f sharing-demo-parameters.yaml \
	-f sharing-demo-claims.yaml \
	-f sharing-demo-job.yaml
```

#### Show processes starting to come down
```console
kubectl get pod -A
```

#### Show MIG devices have been deleted
```console
nvidia-smi -L
```
