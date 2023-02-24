minikube start \
	--driver=none \
	--apiserver-ips=127.0.0.1 \
	--apiserver-name=localhost \
	--force-systemd=true \
	--container-runtime=containerd \
	--extra-config=apiserver.runtime-config=resource.k8s.io/v1alpha1 \
	--feature-gates=DynamicResourceAllocation=true

run-one-until-success kubectl get node 
NODE=$(kubectl get nodes -o=jsonpath='{.items[0].metadata.name}')
kubectl label node ${NODE} --overwrite nvidia.com/dra.controller=true
kubectl label node ${NODE} --overwrite nvidia.com/dra.kubelet-plugin=true
