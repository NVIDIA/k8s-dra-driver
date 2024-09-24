/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VfioPciConfig holds the set of parameters for configuring a GPU in passthrough-mode.
type VfioPciConfig struct {
	metav1.TypeMeta `json:",inline"`
}

// DefaultVfioPciConfig provides the default GPU configuration.
func DefaultVfioPciConfig() *VfioPciConfig {
	return &VfioPciConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       VfioPciConfigKind,
		},
	}
}

// Normalize updates a VfioPciConfig config with implied default values based on other settings.
func (c *VfioPciConfig) Normalize() error {
	return nil
}

// Validate ensures that VfioPciConfig has a valid set of values.
func (c *VfioPciConfig) Validate() error {
	return nil
}
