kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.plugin=true
kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.controller=true
helm upgrade -i nvidia-dra-driver ../deployments/helm/k8s-dra-driver
