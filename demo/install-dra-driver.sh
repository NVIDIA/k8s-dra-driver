kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.plugin=true
kubectl label node 127.0.0.1 --overwrite nvidia.com/dra.controller=true
kubectl apply -f crds/; kubectl apply -f driver.yaml
