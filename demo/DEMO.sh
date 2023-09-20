# Copyright 2023 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Show current state of the cluster
kubectl get pod -A

# Show the current MIG configuration of the machine
nvidia-smi --query-gpu=index,name,uuid,mig.mode.current --format=csv
nvidia-smi -L

# Deploy the 4 example apps discussed in the slides
kubectl apply --filename=gpu-test{1,2,3,4}.yaml

# Show all the pods starting up
kubectl get pod -A

# Show the yaml files for the first 3 example apps
vim -O gpu-test1.yaml gpu-test2.yaml gpu-test3.yaml

# Show the GPUs allocated to each
kubectl logs -n gpu-test1 -l app=pod
kubectl logs -n gpu-test2 pod --all-containers
kubectl logs -n gpu-test3 -l app=pod

# Show the yaml file for the complicated example with MIG devices
vim -O gpu-test4.yaml

# Show the pods running
kubectl get pod -A

# Show the output of nvidia-smi
nvidia-smi -L

# Show the MIG devices allocated to each pod
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

# Delete this example
kubectl delete -f gpu-test4.yaml

# Show the pods terminating
kubectl get pod -A

# Show the output of nvidia-smi
nvidia-smi -L
