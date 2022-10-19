# Reset environment
exec 3< <(sudo ./run-k8s.sh 2>&1)
sudo -E nvidia-mig-parted apply -f mig-parted-config.yaml -c half-half
run-one-until-success cp /var/run/kubernetes/admin.kubeconfig ~/.kube/config
run-one-until-success kubectl get node 127.0.0.1
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.24.0/manifests/calico.yaml
kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.plugin=true
kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.controller=true
kubectl apply -f crds; kubectl apply -f driver.yaml
cat /dev/fd/3
