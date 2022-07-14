set -e

export VERSION=v0.1.0

REGISTRY=nvcr.io/nvidia/cloud-native
IMAGE=k8s-dra-driver
PLATFORM=ubi8

make -f deployments/container/Makefile build-${PLATFORM}
docker tag ${REGISTRY}/${IMAGE}:${VERSION}-${PLATFORM} ${REGISTRY}/${IMAGE}:${VERSION}
docker save ${REGISTRY}/${IMAGE}:${VERSION}-${PLATFORM} > image.tgz
sudo ctr -n k8s.io image import image.tgz
docker save ${REGISTRY}/${IMAGE}:${VERSION} > image.tgz
sudo ctr -n k8s.io image import image.tgz
