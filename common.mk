# Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

GOLANG_VERSION ?= 1.19.2
CUDA_VERSION ?= 11.8.0

DRIVER_NAME := k8s-dra-driver
MODULE := github.com/NVIDIA/$(DRIVER_NAME)

VERSION  ?= v0.1.0
vVERSION := v$(VERSION:v%=%)

VENDOR := nvidia.com

API_BASE := api/$(VENDOR)/resource
PKG_BASE := pkg/$(VENDOR)/resource

CLIENT_APIS := gpu/nas/v1alpha1 gpu/v1alpha1
CLIENT_SOURCES += $(patsubst %, $(API_BASE)/%, $(CLIENT_APIS))

DEEPCOPY_SOURCES = $(CLIENT_SOURCES)

PLURAL_EXCEPTIONS  = DeviceClassParameters:DeviceClassParameters
PLURAL_EXCEPTIONS += GpuClaimParameters:GpuClaimParameters
PLURAL_EXCEPTIONS += MigDeviceClaimParameters:MigDeviceClaimParameters
PLURAL_EXCEPTIONS += ComputeInstanceParameters:ComputeInstanceParameters

ifeq ($(IMAGE_NAME),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE_NAME = $(REGISTRY)/k8s-dra-driver
endif
