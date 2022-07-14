# Set up alias for kubectl
alias kubectl="KUBECONFIG=/var/run/kubernetes/admin.kubeconfig /home/ngc-auth-ldap-kklues/kubernetes/cluster/kubectl.sh"

# Show cluster running no driver
kubectl get pod -A

# Show driver yaml
vim driver.yaml

# Deploy driver
kubectl apply -f driver.yaml

# Show cluster running driver
kubectl get pod -A

# Show GPU configuration on machine
nvidia-smi -L

# Show contents of Gpu CRD
kubectl get gpus -A
kubectl describe gpus -A

# Open pod examples
vim -O pod-example1.yaml pod-example2.yaml pod-example3.yaml

# Deploy pod example 1
kubectl apply -f pod-example1.yaml

# Show cluster running pod example 1
kubectl get pod -A

# Show contents of Gpu CRD
kubectl describe gpus -A

# Show node checkpoint information
sudo cat /var/lib/kubelet/plugins/dra.nvidia.com/checkpoint.json | jq

# Show output of nvidia-smi in deployed containers
kubectl exec gpu-example1-test1 -c ctr -- nvidia-smi -L
kubectl exec gpu-example1-test2 -c ctr -- nvidia-smi -L

# Deploy pod example 2
kubectl apply -f pod-example2.yaml

# Show cluster running pod example 2
kubectl get pod -A

# Show results
kubectl describe gpus -A
sudo cat /var/lib/kubelet/plugins/dra.nvidia.com/checkpoint.json | jq
kubectl exec gpu-example2-test -c ctr1 -- nvidia-smi -L
kubectl exec gpu-example2-test -c ctr2 -- nvidia-smi -L

# Deploy pod example 3
kubectl apply -f pod-example3.yaml

# Show cluster perpetually pending for pod example 3
kubectl get pod -A

# Show logs of controller
kubectl logs -n nvidia-dra-driver   deployment/nvidia-dra-controller

# Delete all pod examples
kubectl delete -f pod-example1.yaml -f pod-example2.yaml -f pod-example3.yaml

# Show not running any more pod examples
kubectl get pod -A

# Show contents of Gpu CRD
kubectl describe gpus -A

# Show node checkpoint information
sudo cat /var/lib/kubelet/plugins/dra.nvidia.com/checkpoint.json | jq


# Extra commands for debugging as necessary...

# Trigger node-plugin to be removed
label node 127.0.0.1 --overwrite nvidia.com/dra.plugin=false

# Trigger node-plugin to be redeployed
label node 127.0.0.1 --overwrite nvidia.com/dra.plugin=true

# Remove node checkpoint file
sudo rm /var/lib/kubelet/plugins/dra.nvidia.com/checkpoint.json
