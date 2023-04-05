# Reset logs and k8s state
sudo rm -rf /tmp/kube*
sudo rm -rf /var/run/kubernetes
sudo rm -rf /var/lib/kubelet

# Start all scripts asynchronously and then wait for each of them to complete
exec 3< <(sudo ./run-k8s.sh 2>&1)
./configure-mig.sh
run-one-until-success cp /var/run/kubernetes/admin.kubeconfig ~/.kube/config
run-one-until-success kubectl get node 127.0.0.1
./install-dra-driver.sh
cat /dev/fd/3
