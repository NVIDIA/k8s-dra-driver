cat <<EOF | sudo -E nvidia-mig-parted apply -f -
version: v1
mig-configs:
  half-half:
    - devices: [0,1,2,3]
      mig-enabled: false
    - devices: [4,5,6,7]
      mig-enabled: true
      mig-devices: {}
EOF
