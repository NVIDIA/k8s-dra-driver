module github.com/NVIDIA/k8s-dra-driver

go 1.19

replace (
	// github.com/NVIDIA/nvidia-container-toolkit => ../nvidia-container-toolkit
	github.com/NVIDIA/nvidia-container-toolkit => gitlab.com/nvidia/container-toolkit/container-toolkit v1.12.0-rc.2.0.20221205163925-c170c4af4307
	gitlab.com/nvidia/cloud-native/go-nvlib => ../go-nvlib
	k8s.io/api => k8s.io/api v0.0.0-20221112014728-9e1815a99d4f
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20221111220531-88105d73321f
	k8s.io/apimachinery => k8s.io/apimachinery v0.27.0-alpha.0
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20221116225048-464f2d738348
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20221108072842-e556445586e6
	k8s.io/client-go => k8s.io/client-go v0.0.0-20221114215055-1ac8d459351e
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20221111221450-016d675f2dd2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20221108081607-5f0e820856c5
	k8s.io/code-generator => k8s.io/code-generator v0.27.0-alpha.0
	k8s.io/component-base => k8s.io/component-base v0.0.0-20221109173154-b1c4f12ee8c1
	k8s.io/component-helpers => k8s.io/component-helpers v0.20.0-alpha.2.0.20221108061658-aa222c251f8e
	k8s.io/cri-api => k8s.io/cri-api v0.27.0-alpha.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20221111221810-c5a029df0b4c
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.26.0-beta.0.0.20221112023518-476733745b57
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20221111220049-0e4f5891cc81
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20221108081024-133329c587a5
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20221108074046-5c32ddd805a4
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20221108094918-a2fdaffa8b2c
	k8s.io/kubectl => k8s.io/kubectl v0.0.0-20221115022131-cf0626ffbf4e
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20221112021749-cf7c23c7af67
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20221111222048-85eda6cdac38
	k8s.io/metrics => k8s.io/metrics v0.0.0-20221108072217-9afa97d4db8e
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20221109054027-aad78e377a8c
)

require (
	github.com/NVIDIA/go-nvml v0.11.6-0.0.20220823120812-7e2082095e82
	github.com/NVIDIA/nvidia-container-toolkit v0.0.0-00010101000000-000000000000
	github.com/container-orchestrated-devices/container-device-interface v0.5.4
	github.com/prometheus/client_golang v1.14.0
	github.com/sirupsen/logrus v1.9.0
	github.com/spf13/cobra v1.6.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.14.0
	gitlab.com/nvidia/cloud-native/go-nvlib v0.0.0-20220922133427-1049a7fa76a9
	k8s.io/api v0.0.0
	k8s.io/apimachinery v0.0.0
	k8s.io/client-go v0.0.0
	k8s.io/component-base v0.0.0
	k8s.io/dynamic-resource-allocation v0.0.0-00010101000000-000000000000
	k8s.io/klog/v2 v2.80.1
	k8s.io/kubelet v0.0.0
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/zapr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.19.14 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/magiconair/properties v1.8.6 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/term v0.0.0-20220808134915-39b0c02b01ae // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runc v1.1.4 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20220825212826-86290f6a00fb // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107090550-2e043c6bd626 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pelletier/go-toml/v2 v2.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/afero v1.9.2 // indirect
	github.com/spf13/cast v1.5.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/subosito/gotenv v1.4.1 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/urfave/cli/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/mod v0.6.0-dev.0.20220419223038-86c51ed26bb4 // indirect
	golang.org/x/net v0.1.1-0.20221027164007-c63010009c80 // indirect
	golang.org/x/oauth2 v0.0.0-20221014153046-6fdb5e3db783 // indirect
	golang.org/x/sys v0.1.0 // indirect
	golang.org/x/term v0.1.0 // indirect
	golang.org/x/text v0.4.0 // indirect
	golang.org/x/time v0.0.0-20220609170525-579cf78fd858 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20221024183307-1bc688fe9f3e // indirect
	google.golang.org/grpc v1.50.1 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280 // indirect
	k8s.io/utils v0.0.0-20221107191617-1a15be271d1d // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
