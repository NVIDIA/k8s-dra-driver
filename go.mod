module github.com/NVIDIA/k8s-dra-driver

go 1.19

replace (
	k8s.io/api => k8s.io/api v0.27.0-beta.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.27.0-beta.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.27.0-beta.0
	k8s.io/apiserver => k8s.io/apiserver v0.27.0-beta.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.27.0-beta.0
	k8s.io/client-go => k8s.io/client-go v0.27.0-beta.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.27.0-beta.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.27.0-beta.0
	k8s.io/code-generator => k8s.io/code-generator v0.27.0-beta.0
	k8s.io/component-base => k8s.io/component-base v0.27.0-beta.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.27.0-beta.0
	k8s.io/cri-api => k8s.io/cri-api v0.27.0-beta.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.27.0-beta.0
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.27.0-beta.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.27.0-beta.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.27.0-beta.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.27.0-beta.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.27.0-beta.0
	k8s.io/kubectl => k8s.io/kubectl v0.27.0-beta.0
	k8s.io/kubelet => k8s.io/kubelet v0.27.0-beta.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.27.0-beta.0
	k8s.io/metrics => k8s.io/metrics v0.27.0-beta.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.27.0-beta.0
)

require (
	github.com/NVIDIA/go-nvml v0.12.0-1
	github.com/NVIDIA/nvidia-container-toolkit v1.13.0-rc.2
	github.com/container-orchestrated-devices/container-device-interface v0.5.4
	github.com/prometheus/client_golang v1.14.0
	github.com/sirupsen/logrus v1.9.0
	github.com/spf13/cobra v1.6.1
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.15.0
	gitlab.com/nvidia/cloud-native/go-nvlib v0.0.0-20230327171225-18ad7cd513cf
	golang.org/x/mod v0.8.0
	k8s.io/api v0.27.0-beta.0
	k8s.io/apimachinery v0.27.0-beta.0
	k8s.io/client-go v0.27.0-beta.0
	k8s.io/component-base v0.27.0-beta.0
	k8s.io/dynamic-resource-allocation v0.0.0-00010101000000-000000000000
	k8s.io/klog/v2 v2.90.1
	k8s.io/kubelet v0.27.0-beta.0
	k8s.io/mount-utils v0.26.3
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/zapr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.1 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/term v0.0.0-20221205130635-1aeaba878587 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runc v1.1.5 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20220825212826-86290f6a00fb // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107090550-2e043c6bd626 // indirect
	github.com/pelletier/go-toml/v2 v2.0.6 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/spf13/afero v1.9.3 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/oauth2 v0.0.0-20221014153046-6fdb5e3db783 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/term v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	golang.org/x/time v0.1.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20221227171554-f9683d7f8bef // indirect
	google.golang.org/grpc v1.52.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20230308215209-15aac26d736a // indirect
	k8s.io/utils v0.0.0-20230209194617-a36077c30491 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
