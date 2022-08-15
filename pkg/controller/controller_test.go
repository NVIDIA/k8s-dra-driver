package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/ktesting"
	_ "k8s.io/klog/v2/ktesting/init"
)

func init() {
	// TODO: remove once klog enables it by default
	klog.EnableContextualLogging(true)
}

func TestController(t *testing.T) {
	claimKey := "claim:default/claim"
	claimName := "claim"
	claimNamespace := "default"
	driverName := "mock-driver"
	className := "mock-class"
	otherDriverName := "other-driver"
	otherClassName := "other-class"
	ourFinalizer := driverName + "/deletion-protection"
	otherFinalizer := otherDriverName + "/deletion-protection"
	classes := []*corev1.ResourceClass{
		createClass(className, driverName),
		createClass(otherClassName, otherDriverName),
	}
	claim := createClaim(claimName, claimNamespace, className)
	otherClaim := createClaim(claimName, claimNamespace, otherClassName)
	delayedClaim := claim.DeepCopy()
	delayedClaim.Spec.AllocationMode = corev1.AllocationModeWaitForFirstConsumer
	podName := "pod"
	podKey := "podscheduling:default/pod"
	pod := createPod(podName, claimNamespace, nil)
	podClaimName := "my-pod-claim"
	podScheduling := createPodScheduling(pod)
	podWithClaim := createPod(podName, claimNamespace, map[string]string{podClaimName: claimName})
	nodeName := "worker"
	otherNodeName := "worker-2"
	unsuitableNodes := []string{otherNodeName}
	potentialNodes := []string{nodeName, otherNodeName}
	withDeletionTimestamp := func(claim *corev1.ResourceClaim) *corev1.ResourceClaim {
		var deleted metav1.Time
		claim = claim.DeepCopy()
		claim.DeletionTimestamp = &deleted
		return claim
	}
	withFinalizer := func(claim *corev1.ResourceClaim, finalizer string) *corev1.ResourceClaim {
		claim = claim.DeepCopy()
		claim.Finalizers = append(claim.Finalizers, finalizer)
		return claim
	}
	allocation := corev1.AllocationResult{}
	withAllocate := func(claim *corev1.ResourceClaim) *corev1.ResourceClaim {
		// Any allocated claim must have our finalizer.
		claim = withFinalizer(claim, ourFinalizer)
		claim.Status.Allocation = &allocation
		claim.Status.DriverName = driverName
		return claim
	}
	withDeallocate := func(claim *corev1.ResourceClaim) *corev1.ResourceClaim {
		claim.Status.DeallocationRequested = true
		return claim
	}
	withSelectedNode := func(podScheduling *corev1.PodScheduling) *corev1.PodScheduling {
		podScheduling = podScheduling.DeepCopy()
		podScheduling.Spec.SelectedNode = nodeName
		return podScheduling
	}
	withUnsuitableNodes := func(podScheduling *corev1.PodScheduling) *corev1.PodScheduling {
		podScheduling = podScheduling.DeepCopy()
		podScheduling.Status.Claims = append(podScheduling.Status.Claims,
			corev1.ResourceClaimSchedulingStatus{PodResourceClaimName: podClaimName, UnsuitableNodes: unsuitableNodes},
		)
		return podScheduling
	}
	withPotentialNodes := func(podScheduling *corev1.PodScheduling) *corev1.PodScheduling {
		podScheduling = podScheduling.DeepCopy()
		podScheduling.Spec.PotentialNodes = potentialNodes
		return podScheduling
	}

	var m mockDriver

	for name, test := range map[string]struct {
		key                                  string
		driver                               mockDriver
		classes                              []*corev1.ResourceClass
		pod                                  *corev1.Pod
		podScheduling, expectedPodScheduling *corev1.PodScheduling
		claim, expectedClaim                 *corev1.ResourceClaim
		expectedError                        string
	}{
		"invalid-key": {
			key:           "claim:x/y/z",
			expectedError: `unexpected key format: "x/y/z"`,
		},
		"not-found": {
			key: "claim:default/claim",
		},
		"wrong-driver": {
			key:           claimKey,
			classes:       classes,
			claim:         otherClaim,
			expectedClaim: otherClaim,
			expectedError: errRequeue.Error(), // class might change
		},
		// Immediate allocation:
		// deletion time stamp set, our finalizer set, not allocated  -> remove finalizer
		"immediate-deleted-finalizer-removal": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(claim), ourFinalizer),
			driver:        m.expectStopAllocation(map[string]error{claimName: nil}),
			expectedClaim: withDeletionTimestamp(claim),
		},
		// deletion time stamp set, our finalizer set, not allocated, stopping fails  -> requeue
		"immediate-deleted-finalizer-stop-failure": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(claim), ourFinalizer),
			driver:        m.expectStopAllocation(map[string]error{claimName: errors.New("fake error")}),
			expectedClaim: withFinalizer(withDeletionTimestamp(claim), ourFinalizer),
			expectedError: "stop allocation: fake error",
		},
		// deletion time stamp set, other finalizer set, not allocated  -> do nothing
		"immediate-deleted-finalizer-no-removal": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(claim), otherFinalizer),
			expectedClaim: withFinalizer(withDeletionTimestamp(claim), otherFinalizer),
		},
		// deletion time stamp set, finalizer set, allocated  -> deallocate
		"immediate-deleted-allocated": {
			key:           claimKey,
			classes:       classes,
			claim:         withAllocate(withDeletionTimestamp(claim)),
			driver:        m.expectDeallocate(map[string]error{claimName: nil}),
			expectedClaim: withDeletionTimestamp(claim),
		},
		// deletion time stamp set, finalizer set, allocated, deallocation fails  -> requeue
		"immediate-deleted-deallocate-failure": {
			key:           claimKey,
			classes:       classes,
			claim:         withAllocate(withDeletionTimestamp(claim)),
			driver:        m.expectDeallocate(map[string]error{claimName: errors.New("fake error")}),
			expectedClaim: withAllocate(withDeletionTimestamp(claim)),
			expectedError: "deallocate: fake error",
		},
		// deletion time stamp set, finalizer not set -> do nothing
		"immediate-deleted-no-finalizer": {
			key:           claimKey,
			classes:       classes,
			claim:         withDeletionTimestamp(claim),
			expectedClaim: withDeletionTimestamp(claim),
		},
		// not deleted, not allocated, no finalizer -> add finalizer, allocate
		"immediate-do-allocation": {
			key:     claimKey,
			classes: classes,
			claim:   claim,
			driver: m.expectClassParameters(map[string]interface{}{className: 1}).
				expectClaimParameters(map[string]interface{}{claimName: 2}).
				expectAllocate(map[string]allocate{claimName: {allocResult: &allocation, allocErr: nil}}),
			expectedClaim: withAllocate(claim),
		},
		// not deleted, not allocated, finalizer -> allocate
		"immediate-continue-allocation": {
			key:     claimKey,
			classes: classes,
			claim:   withFinalizer(claim, ourFinalizer),
			driver: m.expectClassParameters(map[string]interface{}{className: 1}).
				expectClaimParameters(map[string]interface{}{claimName: 2}).
				expectAllocate(map[string]allocate{claimName: {allocResult: &allocation, allocErr: nil}}),
			expectedClaim: withAllocate(claim),
		},
		// not deleted, not allocated, finalizer, fail allocation -> requeue
		"immediate-fail-allocation": {
			key:     claimKey,
			classes: classes,
			claim:   withFinalizer(claim, ourFinalizer),
			driver: m.expectClassParameters(map[string]interface{}{className: 1}).
				expectClaimParameters(map[string]interface{}{claimName: 2}).
				expectAllocate(map[string]allocate{claimName: {allocErr: errors.New("fake error")}}),
			expectedClaim: withFinalizer(claim, ourFinalizer),
			expectedError: "allocate: fake error",
		},
		// not deleted, allocated -> do nothing
		"immediate-allocated-nop": {
			key:           claimKey,
			classes:       classes,
			claim:         withAllocate(claim),
			expectedClaim: withAllocate(claim),
		},

		// not deleted, reallocate -> deallocate
		"immediate-allocated-reallocate": {
			key:           claimKey,
			classes:       classes,
			claim:         withDeallocate(withAllocate(claim)),
			driver:        m.expectDeallocate(map[string]error{claimName: nil}),
			expectedClaim: claim,
		},

		// not deleted, reallocate, deallocate failure -> requeue
		"immediate-allocated-fail-deallocation-during-reallocate": {
			key:           claimKey,
			classes:       classes,
			claim:         withDeallocate(withAllocate(claim)),
			driver:        m.expectDeallocate(map[string]error{claimName: errors.New("fake error")}),
			expectedClaim: withDeallocate(withAllocate(claim)),
			expectedError: "deallocate: fake error",
		},

		// Delayed allocation is similar in some cases, but not quite
		// the same.
		// deletion time stamp set, our finalizer set, not allocated  -> remove finalizer
		"delayed-deleted-finalizer-removal": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(delayedClaim), ourFinalizer),
			driver:        m.expectStopAllocation(map[string]error{claimName: nil}),
			expectedClaim: withDeletionTimestamp(delayedClaim),
		},
		// deletion time stamp set, our finalizer set, not allocated, stopping fails  -> requeue
		"delayed-deleted-finalizer-stop-failure": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(delayedClaim), ourFinalizer),
			driver:        m.expectStopAllocation(map[string]error{claimName: errors.New("fake error")}),
			expectedClaim: withFinalizer(withDeletionTimestamp(delayedClaim), ourFinalizer),
			expectedError: "stop allocation: fake error",
		},
		// deletion time stamp set, other finalizer set, not allocated  -> do nothing
		"delayed-deleted-finalizer-no-removal": {
			key:           claimKey,
			classes:       classes,
			claim:         withFinalizer(withDeletionTimestamp(delayedClaim), otherFinalizer),
			expectedClaim: withFinalizer(withDeletionTimestamp(delayedClaim), otherFinalizer),
		},
		// deletion time stamp set, finalizer set, allocated  -> deallocate
		"delayed-deleted-allocated": {
			key:           claimKey,
			classes:       classes,
			claim:         withAllocate(withDeletionTimestamp(delayedClaim)),
			driver:        m.expectDeallocate(map[string]error{claimName: nil}),
			expectedClaim: withDeletionTimestamp(delayedClaim),
		},
		// deletion time stamp set, finalizer set, allocated, deallocation fails  -> requeue
		"delayed-deleted-deallocate-failure": {
			key:           claimKey,
			classes:       classes,
			claim:         withAllocate(withDeletionTimestamp(delayedClaim)),
			driver:        m.expectDeallocate(map[string]error{claimName: errors.New("fake error")}),
			expectedClaim: withAllocate(withDeletionTimestamp(delayedClaim)),
			expectedError: "deallocate: fake error",
		},
		// deletion time stamp set, finalizer not set -> do nothing
		"delayed-deleted-no-finalizer": {
			key:           claimKey,
			classes:       classes,
			claim:         withDeletionTimestamp(delayedClaim),
			expectedClaim: withDeletionTimestamp(delayedClaim),
		},
		// waiting for first consumer -> do nothing
		"delayed-pending": {
			key:           claimKey,
			classes:       classes,
			claim:         delayedClaim,
			expectedClaim: delayedClaim,
		},

		// pod with no claims -> shouldn't occur, check again anyway
		"pod-nop": {
			key:                   podKey,
			pod:                   pod,
			podScheduling:         withSelectedNode(podScheduling),
			expectedPodScheduling: withSelectedNode(podScheduling),
			expectedError:         errPeriodic.Error(),
		},

		// pod with immediate allocation and selected node -> shouldn't occur, check again in case that claim changes
		"pod-immediate": {
			key:                   podKey,
			claim:                 claim,
			expectedClaim:         claim,
			pod:                   podWithClaim,
			podScheduling:         withSelectedNode(podScheduling),
			expectedPodScheduling: withSelectedNode(podScheduling),
			expectedError:         errPeriodic.Error(),
		},

		// pod with delayed allocation, no potential nodes -> shouldn't occur
		"pod-delayed-no-nodes": {
			key:                   podKey,
			classes:               classes,
			claim:                 delayedClaim,
			expectedClaim:         delayedClaim,
			pod:                   podWithClaim,
			podScheduling:         podScheduling,
			expectedPodScheduling: podScheduling,
		},

		// pod with delayed allocation, potential nodes -> provide unsuitable nodes
		"pod-delayed-info": {
			key:           podKey,
			classes:       classes,
			claim:         delayedClaim,
			expectedClaim: delayedClaim,
			pod:           podWithClaim,
			podScheduling: withPotentialNodes(podScheduling),
			driver: m.expectClassParameters(map[string]interface{}{className: 1}).
				expectClaimParameters(map[string]interface{}{claimName: 2}).
				expectUnsuitableNodes(map[string][]string{podClaimName: unsuitableNodes}, nil),
			expectedPodScheduling: withUnsuitableNodes(withPotentialNodes(podScheduling)),
			expectedError:         errPeriodic.Error(),
		},

		// pod with delayed allocation, potential nodes, selected node, missing class -> failure
		"pod-delayed-missing-class": {
			key:                   podKey,
			claim:                 delayedClaim,
			expectedClaim:         delayedClaim,
			pod:                   podWithClaim,
			podScheduling:         withSelectedNode(withPotentialNodes(podScheduling)),
			expectedPodScheduling: withSelectedNode(withPotentialNodes(podScheduling)),
			expectedError:         `pod claim my-pod-claim: resourceclass "mock-class" not found`,
		},

		// pod with delayed allocation, potential nodes, selected node -> allocate, provide unsuitable nodes
		"pod-delayed-allocate": {
			key:           podKey,
			classes:       classes,
			claim:         delayedClaim,
			expectedClaim: withAllocate(delayedClaim),
			pod:           podWithClaim,
			podScheduling: withSelectedNode(withPotentialNodes(podScheduling)),
			driver: m.expectClassParameters(map[string]interface{}{className: 1}).
				expectClaimParameters(map[string]interface{}{claimName: 2}).
				expectUnsuitableNodes(map[string][]string{podClaimName: unsuitableNodes}, nil).
				expectAllocate(map[string]allocate{claimName: {allocResult: &allocation, selectedNode: nodeName, allocErr: nil}}),
			expectedPodScheduling: withUnsuitableNodes(withSelectedNode(withPotentialNodes(podScheduling))),
			expectedError:         errPeriodic.Error(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, ctx := ktesting.NewTestContext(t)
			initialObjects := []runtime.Object{}
			for _, class := range test.classes {
				initialObjects = append(initialObjects, class)
			}
			if test.pod != nil {
				initialObjects = append(initialObjects, test.pod)
			}
			if test.podScheduling != nil {
				initialObjects = append(initialObjects, test.podScheduling)
			}
			if test.claim != nil {
				initialObjects = append(initialObjects, test.claim)
			}
			kubeClient, informerFactory := fakeK8s(initialObjects)
			rcInformer := informerFactory.Core().V1().ResourceClasses()
			claimInformer := informerFactory.Core().V1().ResourceClaims()
			podInformer := informerFactory.Core().V1().Pods()
			podSchedulingInformer := informerFactory.Core().V1().PodSchedulings()

			for _, obj := range initialObjects {
				switch obj.(type) {
				case *corev1.ResourceClass:
					rcInformer.Informer().GetStore().Add(obj)
				case *corev1.ResourceClaim:
					claimInformer.Informer().GetStore().Add(obj)
				case *corev1.Pod:
					podInformer.Informer().GetStore().Add(obj)
				case *corev1.PodScheduling:
					podSchedulingInformer.Informer().GetStore().Add(obj)
				default:
					t.Fatalf("unknown initalObject type: %+v", obj)
				}
			}

			driver := test.driver
			driver.t = t

			ctrl := New(ctx, driverName, driver, kubeClient, informerFactory)
			_, err := ctrl.(*controller).syncKey(ctx, test.key)
			if err != nil && test.expectedError == "" {
				t.Fatalf("unexpected error: %v", err)
			}
			if err == nil && test.expectedError != "" {
				t.Fatalf("did not get expected error %q", test.expectedError)
			}
			if err != nil && err.Error() != test.expectedError {
				t.Fatalf("expected error %q, got %q", test.expectedError, err.Error())
			}
			claims, err := kubeClient.CoreV1().ResourceClaims("").List(ctx, metav1.ListOptions{})
			require.NoError(t, err, "list claims")
			var expectedClaims []corev1.ResourceClaim
			if test.expectedClaim != nil {
				expectedClaims = append(expectedClaims, *test.expectedClaim)
			}
			assert.Equal(t, expectedClaims, claims.Items)

			podSchedulings, err := kubeClient.CoreV1().PodSchedulings("").List(ctx, metav1.ListOptions{})
			require.NoError(t, err, "list pod schedulings")
			var expectedPodSchedulings []corev1.PodScheduling
			if test.expectedPodScheduling != nil {
				expectedPodSchedulings = append(expectedPodSchedulings, *test.expectedPodScheduling)
			}
			assert.Equal(t, expectedPodSchedulings, podSchedulings.Items)

			// TODO: add testing of events.
			// Right now, client-go/tools/record/event.go:267 fails during unit testing with
			// request namespace does not match object namespace, request: "" object: "default",
		})
	}
}

func asClaim(t *testing.T, objs []interface{}) *corev1.ResourceClaim {
	switch len(objs) {
	case 0:
		return nil
	case 1:
		return objs[0].(*corev1.ResourceClaim)
	default:
		t.Helper()
		t.Fatalf("expected at most one ResourceClaim, got: %v", objs)
		return nil
	}
}

type mockDriver struct {
	t *testing.T

	// TODO: change this so that the mock driver expects calls in a certain order
	// and fails when the next call isn't the expected one or calls didn't happen
	classParameters      map[string]interface{}
	claimParameters      map[string]interface{}
	allocate             map[string]allocate
	deallocate           map[string]error
	stopAllocation       map[string]error
	unsuitableNodes      map[string][]string
	unsuitableNodesError error
}

type allocate struct {
	selectedNode string
	allocResult  *corev1.AllocationResult
	allocErr     error
}

func (m mockDriver) expectClassParameters(expected map[string]interface{}) mockDriver {
	m.classParameters = expected
	return m
}

func (m mockDriver) expectClaimParameters(expected map[string]interface{}) mockDriver {
	m.claimParameters = expected
	return m
}

func (m mockDriver) expectAllocate(expected map[string]allocate) mockDriver {
	m.allocate = expected
	return m
}

func (m mockDriver) expectStopAllocation(expected map[string]error) mockDriver {
	m.stopAllocation = expected
	return m
}

func (m mockDriver) expectDeallocate(expected map[string]error) mockDriver {
	m.deallocate = expected
	return m
}

func (m mockDriver) expectUnsuitableNodes(expected map[string][]string, err error) mockDriver {
	m.unsuitableNodes = expected
	m.unsuitableNodesError = err
	return m
}

func (m mockDriver) GetClassParameters(ctx context.Context, class *corev1.ResourceClass) (interface{}, error) {
	m.t.Logf("GetClassParameters(%s)", class)
	result, ok := m.classParameters[class.Name]
	if !ok {
		m.t.Fatal("unexpected GetClassParameters call")
	}
	if err, ok := result.(error); ok {
		return nil, err
	}
	return result, nil
}

func (m mockDriver) GetClaimParameters(ctx context.Context, claim *corev1.ResourceClaim, class *corev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	m.t.Logf("GetClaimParameters(%s)", claim)
	result, ok := m.claimParameters[claim.Name]
	if !ok {
		m.t.Fatal("unexpected GetClaimParameters call")
	}
	if err, ok := result.(error); ok {
		return nil, err
	}
	return result, nil
}

func (m mockDriver) Allocate(ctx context.Context, claim *corev1.ResourceClaim, claimParameters interface{}, class *corev1.ResourceClass, classParameters interface{}, selectedNode string) (*corev1.AllocationResult, error) {
	m.t.Logf("Allocate(%s)", claim)
	allocate, ok := m.allocate[claim.Name]
	if !ok {
		m.t.Fatal("unexpected Allocate call")
	}
	assert.Equal(m.t, allocate.selectedNode, selectedNode, "selected node")
	return allocate.allocResult, allocate.allocErr
}

func (m mockDriver) StopAllocation(ctx context.Context, claim *corev1.ResourceClaim) error {
	m.t.Logf("StopAllocation(%s)", claim)
	err, ok := m.stopAllocation[claim.Name]
	if !ok {
		m.t.Fatal("unexpected StopAllocation call")
	}
	return err
}

func (m mockDriver) Deallocate(ctx context.Context, claim *corev1.ResourceClaim) error {
	m.t.Logf("Deallocate(%s)", claim)
	err, ok := m.deallocate[claim.Name]
	if !ok {
		m.t.Fatal("unexpected Deallocate call")
	}
	return err
}

func (m mockDriver) UnsuitableNodes(ctx context.Context, pod *v1.Pod, claims []*ClaimAllocation, potentialNodes []string) error {
	m.t.Logf("UnsuitableNodes(%s, %v, %v)", pod, claims, potentialNodes)
	if len(m.unsuitableNodes) == 0 {
		m.t.Fatal("unexpected UnsuitableNodes call")
	}
	if m.unsuitableNodesError != nil {
		return m.unsuitableNodesError
	}
	found := map[string]bool{}
	for _, delayed := range claims {
		unsuitableNodes, ok := m.unsuitableNodes[delayed.PodClaimName]
		if !ok {
			m.t.Errorf("unexpected pod claim: %s", delayed.PodClaimName)
		}
		delayed.UnsuitableNodes = unsuitableNodes
		found[delayed.PodClaimName] = true
	}
	for expectedName := range m.unsuitableNodes {
		if !found[expectedName] {
			m.t.Errorf("pod claim %s not in actual claims list", expectedName)
		}
	}
	return nil
}

func createClass(className, driverName string) *corev1.ResourceClass {
	return &corev1.ResourceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: className,
		},
		DriverName: driverName,
	}
}

func createClaim(claimName, claimNamespace, className string) *corev1.ResourceClaim {
	return &corev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: claimNamespace,
		},
		Spec: corev1.ResourceClaimSpec{
			ResourceClassName: className,
			AllocationMode:    corev1.AllocationModeImmediate,
		},
	}
}

func createPod(podName, podNamespace string, claims map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
		},
	}
	for podClaimName, claimName := range claims {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims,
			corev1.PodResourceClaim{
				Name: podClaimName,
				Claim: corev1.ClaimSource{
					ResourceClaimName: &claimName,
				},
			},
		)
	}
	return pod
}

func createPodScheduling(pod *corev1.Pod) *corev1.PodScheduling {
	controller := true
	return &corev1.PodScheduling{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       pod.Name,
					Controller: &controller,
				},
			},
		},
	}
}

func fakeK8s(objs []runtime.Object) (kubernetes.Interface, informers.SharedInformerFactory) {
	// This is a very simple replacement for a real apiserver. For example,
	// it doesn't do defaulting and accepts updates to the status in normal
	// Update calls. Therefore this test does not catch when we use Update
	// instead of UpdateStatus. Reactors could be used to catch that, but
	// that seems overkill because E2E tests will find that.
	//
	// Interactions with the fake apiserver also never fail. TODO:
	// simulate update errors.
	client := fake.NewSimpleClientset(objs...)
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	return client, informerFactory
}
