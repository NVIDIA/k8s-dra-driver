/*
 * Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
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

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"

	nascrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/nas/v1alpha1"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	cdispec "github.com/container-orchestrated-devices/container-device-interface/specs-go"
)

const (
	MpsRoot                      = DriverPluginPath + "/mps"
	MpsControlDaemonTemplatePath = "/templates/mps-control-daemon.tmpl.yaml"
	MpsControlDaemonNameFmt      = "mps-control-daemon-%v" // Fill with ClaimUID
)

type TimeSlicingManager struct {
	nvidiaDriverRoot string
}

type MpsManager struct {
	config           *Config
	controlFilesRoot string
	nvidiaDriverRoot string
	templatePath     string
}

type MpsControlDaemon struct {
	namespace string
	name      string
	rootDir   string
	pipeDir   string
	shmDir    string
	logDir    string
	claim     *nascrd.ClaimInfo
	devices   *AllocatedDevices
	config    *nascrd.MpsConfig
	manager   *MpsManager
}

type MpsControlDaemonTemplateData struct {
	MpsControlDaemonNamespace         string
	MpsControlDaemonName              string
	CUDA_VISIBLE_DEVICES              string
	CUDA_DEVICE_MAX_CONNECTIONS       string
	CUDA_MPS_ACTIVE_THREAD_PERCENTAGE string
	CUDA_MPS_PINNED_DEVICE_MEM_LIMIT  string
	NvidiaDriverRoot                  string
	MpsShmDirectory                   string
	MpsPipeDirectory                  string
	MpsLogDirectory                   string
}

func NewTimeSlicingManager(nvidiaDriverRoot string) *TimeSlicingManager {
	return &TimeSlicingManager{
		nvidiaDriverRoot: nvidiaDriverRoot,
	}
}

func (t *TimeSlicingManager) SetTimeSlice(devices *AllocatedDevices, config *nascrd.TimeSlicingConfig) error {
	if devices.Mig != nil {
		return fmt.Errorf("setting a TimeSlice duration on MIG devices is unsupported")
	}

	timeSlice := nascrd.DefaultTimeSlice
	if config != nil && config.TimeSlice != nil {
		timeSlice = *config.TimeSlice
	}

	err := setComputeMode(t.nvidiaDriverRoot, devices.UUIDs(), "DEFAULT")
	if err != nil {
		return fmt.Errorf("error setting compute mode: %v", err)
	}

	err = setTimeSlice(t.nvidiaDriverRoot, devices.UUIDs(), timeSlice.Int())
	if err != nil {
		return fmt.Errorf("error setting time slice: %v", err)
	}

	return nil
}

func NewMpsManager(config *Config, controlFilesRoot, nvidiaDriverRoot, templatePath string) *MpsManager {
	return &MpsManager{
		controlFilesRoot: controlFilesRoot,
		nvidiaDriverRoot: nvidiaDriverRoot,
		templatePath:     templatePath,
		config:           config,
	}
}

func (m *MpsManager) NewMpsControlDaemon(claim *nascrd.ClaimInfo, devices *AllocatedDevices, config *nascrd.MpsConfig) *MpsControlDaemon {
	return &MpsControlDaemon{
		namespace: m.config.nascrd.Namespace,
		name:      fmt.Sprintf(MpsControlDaemonNameFmt, claim.UID),
		claim:     claim,
		rootDir:   fmt.Sprintf("%s/%s", m.controlFilesRoot, claim.UID),
		pipeDir:   fmt.Sprintf("%s/%s/%s", m.controlFilesRoot, claim.UID, "pipe"),
		shmDir:    fmt.Sprintf("%s/%s/%s", m.controlFilesRoot, claim.UID, "shm"),
		logDir:    fmt.Sprintf("%s/%s/%s", m.controlFilesRoot, claim.UID, "log"),
		devices:   devices,
		config:    config,
		manager:   m,
	}
}

func (m *MpsManager) IsControlDaemonStarted(claim *nascrd.ClaimInfo) (bool, error) {
	name := fmt.Sprintf(MpsControlDaemonNameFmt, claim.UID)
	_, err := m.config.clientset.core.AppsV1().Deployments(m.config.nascrd.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get deployment: %v", err)
	}
	return true, nil
}

func (m *MpsManager) IsControlDaemonStopped(claim *nascrd.ClaimInfo) (bool, error) {
	name := fmt.Sprintf(MpsControlDaemonNameFmt, claim.UID)
	_, err := m.config.clientset.core.AppsV1().Deployments(m.config.nascrd.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get deployment: %v", err)
	}
	return false, nil
}

func (m *MpsControlDaemon) Start() error {
	isStarted, err := m.manager.IsControlDaemonStarted(m.claim)
	if err != nil {
		fmt.Errorf("error checking if control daemon already started")
	}

	if isStarted {
		return nil
	}

	klog.Infof("Starting MPS control daemon for '%v', with settings: %+v", m.claim.UID, m.config)

	templateData := MpsControlDaemonTemplateData{
		MpsControlDaemonNamespace:         m.namespace,
		MpsControlDaemonName:              m.name,
		CUDA_VISIBLE_DEVICES:              strings.Join(m.devices.UUIDs(), ","),
		CUDA_DEVICE_MAX_CONNECTIONS:       "",
		CUDA_MPS_ACTIVE_THREAD_PERCENTAGE: "",
		CUDA_MPS_PINNED_DEVICE_MEM_LIMIT:  "",
		NvidiaDriverRoot:                  m.manager.nvidiaDriverRoot,
		MpsShmDirectory:                   m.shmDir,
		MpsPipeDirectory:                  m.pipeDir,
		MpsLogDirectory:                   m.logDir,
	}

	if m.config != nil && m.config.MaxConnections != nil {
		templateData.CUDA_DEVICE_MAX_CONNECTIONS = fmt.Sprintf("%v", m.config.MaxConnections)
	}

	if m.config != nil && m.config.ActiveThreadPercentage != nil {
		templateData.CUDA_MPS_ACTIVE_THREAD_PERCENTAGE = fmt.Sprintf("%v", m.config.ActiveThreadPercentage)
	}

	if m.config != nil && m.config.PinnedDeviceMemoryLimit != nil {
		limits, err := m.config.PinnedDeviceMemoryLimit.String()
		if err != nil {
			return fmt.Errorf("error transforming PinnedDeviceMemoryLimit into string: %v", err)
		}
		templateData.CUDA_MPS_PINNED_DEVICE_MEM_LIMIT = limits
	}

	tmpl, err := template.ParseFiles(m.manager.templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template file: %v", err)
	}

	var deploymentYaml bytes.Buffer
	if err := tmpl.Execute(&deploymentYaml, templateData); err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	var unstructuredObj unstructured.Unstructured
	err = yaml.Unmarshal(deploymentYaml.Bytes(), &unstructuredObj)
	if err != nil {
		return fmt.Errorf("failed to unmarshal yaml: %v", err)
	}

	var deployment appsv1.Deployment
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), &deployment)
	if err != nil {
		return fmt.Errorf("failed to convert unstructured data to typed object: %v", err)
	}

	err = os.MkdirAll(m.shmDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating directory %v: %v", m.shmDir, err)
	}

	err = os.MkdirAll(m.pipeDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating directory %v: %v", m.pipeDir, err)
	}

	err = os.MkdirAll(m.logDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating directory %v: %v", m.logDir, err)
	}

	mountExecutable, err := exec.LookPath("mount")
	if err != nil {
		return fmt.Errorf("error finding 'mount' executable: %v", err)
	}

	mounter := mount.New(mountExecutable)
	mountOptions := []string{"rw", "nosuid", "nodev", "noexec", "relatime", "size=65536k"}
	err = mounter.Mount("shm", m.shmDir, "tmpfs", mountOptions)
	if err != nil {
		return fmt.Errorf("error mounting %v as tmpfs: %v", m.shmDir, err)
	}

	if m.devices.Type() == nascrd.GpuDeviceType {
		err = setComputeMode(m.manager.nvidiaDriverRoot, m.devices.UUIDs(), "EXCLUSIVE_PROCESS")
		if err != nil {
			return fmt.Errorf("error setting compute mode: %v", err)
		}
	}

	_, err = m.manager.config.clientset.core.AppsV1().Deployments(m.namespace).Create(context.TODO(), &deployment, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	return nil
}

func (m *MpsControlDaemon) AssertReady() error {
	backoff := wait.Backoff{
		Duration: time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    4,
		Cap:      10 * time.Second,
	}

	return retry.OnError(
		backoff,
		func(error) bool {
			return true
		},
		func() error {
			deployment, err := m.manager.config.clientset.core.AppsV1().Deployments(m.namespace).Get(
				context.TODO(),
				m.name,
				metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to get deployment: %v", err)
			}

			if deployment.Status.ReadyReplicas != 1 {
				return fmt.Errorf("waiting for MPS control daemon to come online")
			}

			selector := deployment.Spec.Selector.MatchLabels

			pods, err := m.manager.config.clientset.core.CoreV1().Pods(m.namespace).List(
				context.TODO(),
				metav1.ListOptions{
					LabelSelector: labels.Set(selector).AsSelector().String(),
				},
			)
			if err != nil {
				return fmt.Errorf("error listing pods from deployment")
			}

			if len(pods.Items) != 1 {
				return fmt.Errorf("unexpected number of pods in deployment: %v", len(pods.Items))
			}

			if len(pods.Items[0].Status.ContainerStatuses) != 1 {
				return fmt.Errorf("unexpected number of container statuses in pod")
			}

			if !pods.Items[0].Status.ContainerStatuses[0].Ready {
				return fmt.Errorf("control daemon not yet ready")
			}

			return nil
		},
	)
}

func (m *MpsControlDaemon) GetCDIContainerEdits() *cdiapi.ContainerEdits {
	return &cdiapi.ContainerEdits{
		&cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("CUDA_MPS_PIPE_DIRECTORY=%s", "/tmp/nvidia-mps"),
			},
			Mounts: []*cdispec.Mount{
				{
					ContainerPath: "/dev/shm",
					HostPath:      m.shmDir,
					Options:       []string{"rw", "nosuid", "nodev", "bind"},
				},
				{
					ContainerPath: "/tmp/nvidia-mps",
					HostPath:      m.pipeDir,
					Options:       []string{"rw", "nosuid", "nodev", "bind"},
				},
			},
		},
	}
}

func (m *MpsControlDaemon) Stop() error {
	_, err := os.Stat(m.rootDir)
	if os.IsNotExist(err) {
		return nil
	}

	klog.Infof("Stopping MPS control daemon for claim '%v'", m.claim.UID)

	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	err = m.manager.config.clientset.core.AppsV1().Deployments(m.namespace).Delete(context.TODO(), m.name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment: %v", err)
	}

	mountExecutable, err := exec.LookPath("mount")
	if err != nil {
		return fmt.Errorf("error finding 'mount' executable: %v", err)
	}

	mounter := mount.New(mountExecutable)
	err = mount.CleanupMountPoint(m.shmDir, mounter, true)
	if err != nil {
		return fmt.Errorf("error unmounting %v: %v", m.shmDir, err)
	}

	err = os.RemoveAll(m.rootDir)
	if err != nil {
		return fmt.Errorf("error removing directory %v: %v", m.rootDir, err)
	}

	return nil
}
