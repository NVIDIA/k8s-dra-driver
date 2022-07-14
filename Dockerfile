FROM golang:1.18.2

ENV GO111MODULE=off

RUN go get k8s.io/code-generator; exit 0
RUN go get k8s.io/apimachinery; exit 0

WORKDIR $GOPATH/src/k8s.io/code-generator

CMD ["./generate-groups.sh", \
     "all", \
     "github.com/NVIDIA/k8s-dra-driver/pkg/crd/nvidia", \
     "github.com/NVIDIA/k8s-dra-driver/pkg/crd", \
     "nvidia:v1", \
     "--go-header-file", "./hack/boilerplate.go.txt"]
