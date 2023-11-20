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

package main

import "k8s.io/klog/v2"

type klogLogger struct{}

var klogAsLogger klogLogger

func (l *klogLogger) Debugf(fmt string, args ...interface{}) {
	// TODO: The debug level needs to be configurable
	klog.V(1).Infof(fmt, args...)
}

func (l *klogLogger) Errorf(fmt string, args ...interface{}) {
	klog.Errorf(fmt, args...)
}

func (l *klogLogger) Info(args ...interface{}) {
	klog.Info(args)
}

func (l *klogLogger) Infof(fmt string, args ...interface{}) {
	klog.Infof(fmt, args...)
}

func (l *klogLogger) Warning(args ...interface{}) {
	klog.Warning(args...)
}

func (l *klogLogger) Warningf(fmt string, args ...interface{}) {
	klog.Warningf(fmt, args...)
}
