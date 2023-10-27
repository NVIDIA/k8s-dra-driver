/**
# Copyright 2023 NVIDIA CORPORATION
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
**/

package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
)

func TestMpsPerDevicePinnedMemoryLimitNormalize(t *testing.T) {
	testCases := []struct {
		description          string
		uuids                []string
		memoryLimit          *resource.Quantity
		perDeviceMemoryLimit v1alpha1.MpsPerDevicePinnedMemoryLimit
		expectedError        error
		expectedLimits       map[string]string
	}{
		{
			description: "no uuids, no default",
			perDeviceMemoryLimit: v1alpha1.MpsPerDevicePinnedMemoryLimit{
				"0": resource.MustParse("1Gi"),
			},
			expectedLimits: map[string]string{
				"0": "1024M",
			},
		},
		{
			description: "no uuids, default is overridden",
			memoryLimit: ptr(resource.MustParse("2Gi")),
			perDeviceMemoryLimit: v1alpha1.MpsPerDevicePinnedMemoryLimit{
				"0": resource.MustParse("1Gi"),
			},
			expectedLimits: map[string]string{
				"0": "1024M",
			},
		},
		{
			description: "uuids, default is set",
			uuids:       []string{"UUID0"},
			memoryLimit: ptr(resource.MustParse("2Gi")),
			expectedLimits: map[string]string{
				"0": "2048M",
			},
		},
		{
			description: "uuids, default is overridden",
			uuids:       []string{"UUID0"},
			memoryLimit: ptr(resource.MustParse("2Gi")),
			perDeviceMemoryLimit: v1alpha1.MpsPerDevicePinnedMemoryLimit{
				"0": resource.MustParse("1Gi"),
			},
			expectedLimits: map[string]string{
				"0": "1024M",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {

			limits, err := tc.perDeviceMemoryLimit.Normalize(tc.uuids, tc.memoryLimit)
			require.ErrorIs(t, err, tc.expectedError)
			require.EqualValues(t, tc.expectedLimits, limits)
		})
	}
}

// prt returns a reference to whatever type is passed into it.
func ptr[T any](x T) *T {
	return &x
}
