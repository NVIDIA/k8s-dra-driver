package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	resourceapi "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	gpucrd "github.com/NVIDIA/k8s-dra-driver/api/nvidia.com/resource/gpu/v1alpha1"
)

func StartClaimParametersGenerator(ctx context.Context, config *Config) error {
	// Build a client set config
	csconfig, err := config.flags.kubeClientConfig.NewClientSetConfig()
	if err != nil {
		return fmt.Errorf("error creating client set config: %w", err)
	}

	// Create a new dynamic client
	dynamicClient, err := dynamic.NewForConfig(csconfig)
	if err != nil {
		return fmt.Errorf("error creating dynamic client: %w", err)
	}

	klog.Info("Starting ResourceClaimParamaters generator")

	// Set up informer to watch for GpuClaimParameters objects
	gpuClaimParametersInformer := newGpuClaimParametersInformer(dynamicClient)

	// Set up handler for events
	gpuClaimParametersInformer.AddEventHandler(newGpuClaimParametersHandler(config.clientSets.Core, dynamicClient))

	// Start informer
	go gpuClaimParametersInformer.Run(ctx.Done())

	return nil
}

func newGpuClaimParametersInformer(dynamicClient dynamic.Interface) cache.SharedIndexInformer {
	// Set up shared index informer for GpuClaimParameters objects
	gvr := schema.GroupVersionResource{
		Group:    gpucrd.GroupName,
		Version:  gpucrd.Version,
		Resource: strings.ToLower(gpucrd.GpuClaimParametersKind),
	}

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return dynamicClient.Resource(gvr).List(context.Background(), metav1.ListOptions{})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return dynamicClient.Resource(gvr).Watch(context.Background(), metav1.ListOptions{})
			},
		},
		&unstructured.Unstructured{},
		0, // resyncPeriod
		cache.Indexers{},
	)

	return informer
}

func newGpuClaimParametersHandler(clientset kubernetes.Interface, dynamicClient dynamic.Interface) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			unstructured := obj.(*unstructured.Unstructured)

			var gpuClaimParameters gpucrd.GpuClaimParameters
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &gpuClaimParameters)
			if err != nil {
				klog.Errorf("Error converting *unstructured.Unstructured to GpuClaimParameters: %v", err)
				return
			}

			if err := createOrUpdateResourceClaimParameters(clientset, &gpuClaimParameters); err != nil {
				klog.Errorf("Error creating ResourceClaimParameters: %v", err)
				return
			}
		},
		UpdateFunc: func(oldObj any, newObj any) {
			unstructured := newObj.(*unstructured.Unstructured)

			var gpuClaimParameters gpucrd.GpuClaimParameters
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &gpuClaimParameters)
			if err != nil {
				klog.Errorf("Error converting *unstructured.Unstructured to GpuClaimParameters: %v", err)
				return
			}

			if err := createOrUpdateResourceClaimParameters(clientset, &gpuClaimParameters); err != nil {
				klog.Errorf("Error updating ResourceClaimParameters: %v", err)
				return
			}
		},
	}
}

func newResourceClaimParametersFromGpuClaimParameters(gpuClaimParameters *gpucrd.GpuClaimParameters) (*resourceapi.ResourceClaimParameters, error) {
	namespace := gpuClaimParameters.Namespace

	rawSpec, err := json.Marshal(gpuClaimParameters.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshaling GpuClaimParamaters to JSON: %w", err)
	}

	resourceCount := 1
	if gpuClaimParameters.Spec.Count != nil {
		resourceCount = *gpuClaimParameters.Spec.Count
	}

	selector := "true"
	if gpuClaimParameters.Spec.Selector != nil {
		selector = gpuClaimParameters.Spec.Selector.ToNamedResourcesSelector()
	}

	shareable := true

	var resourceRequests []resourceapi.ResourceRequest
	for i := 0; i < resourceCount; i++ {
		request := resourceapi.ResourceRequest{
			ResourceRequestModel: resourceapi.ResourceRequestModel{
				NamedResources: &resourceapi.NamedResourcesRequest{
					Selector: selector,
				},
			},
		}
		resourceRequests = append(resourceRequests, request)
	}

	resourceClaimParameters := &resourceapi.ResourceClaimParameters{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "resource-claim-parameters-",
			Namespace:    namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gpuClaimParameters.APIVersion,
					Kind:               gpuClaimParameters.Kind,
					Name:               gpuClaimParameters.Name,
					UID:                gpuClaimParameters.UID,
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		GeneratedFrom: &resourceapi.ResourceClaimParametersReference{
			APIGroup: gpucrd.GroupName,
			Kind:     gpuClaimParameters.Kind,
			Name:     gpuClaimParameters.Name,
		},
		DriverRequests: []resourceapi.DriverRequests{
			{
				DriverName:       DriverName,
				VendorParameters: runtime.RawExtension{Raw: rawSpec},
				Requests:         resourceRequests,
			},
		},
		Shareable: shareable,
	}

	return resourceClaimParameters, nil
}

func createOrUpdateResourceClaimParameters(clientset kubernetes.Interface, gpuClaimParameters *gpucrd.GpuClaimParameters) error {
	namespace := gpuClaimParameters.Namespace

	// Get a list of existing ResourceClaimParameters in the same namespace as the incoming GpuClaimParameters
	existing, err := clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing existing ResourceClaimParameters: %w", err)
	}

	// Build a new ResourceClaimParameters object from the incoming GpuClaimParameters object
	resourceClaimParameters, err := newResourceClaimParametersFromGpuClaimParameters(gpuClaimParameters)
	if err != nil {
		return fmt.Errorf("error building new ResourceClaimParameters object from a GpuClaimParameters object: %w", err)
	}

	// If there is an existing ResourceClaimParameters generated from the incoming GpuClaimParameters object, then update it
	if len(existing.Items) > 0 {
		for _, item := range existing.Items {
			if (item.GeneratedFrom.APIGroup == gpucrd.GroupName) &&
				(item.GeneratedFrom.Kind == gpuClaimParameters.Kind) &&
				(item.GeneratedFrom.Name == gpuClaimParameters.Name) {
				klog.Infof("ResourceClaimParameters already exists for GpuClaimParameters %s/%s, updating it", namespace, gpuClaimParameters.Name)

				// Copy the matching ResourceClaimParameters metadata into the new ResourceClaimParameters object before updating it
				resourceClaimParameters.ObjectMeta = *item.ObjectMeta.DeepCopy()

				_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Update(context.TODO(), resourceClaimParameters, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("error updating ResourceClaimParameters object: %w", err)
				}

				return nil
			}
		}
	}

	// Otherwise create a new ResourceClaimParameters object from the incoming GpuClaimParameters object
	_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Create(context.TODO(), resourceClaimParameters, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating ResourceClaimParameters object from GpuClaimParameters object: %w", err)
	}

	klog.Infof("Created ResourceClaimParameters for GpuClaimParameters %s/%s", namespace, gpuClaimParameters.Name)
	return nil
}
