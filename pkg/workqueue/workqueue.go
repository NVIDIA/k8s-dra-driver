/*
 * Copyright (c) 2025 NVIDIA CORPORATION.  All rights reserved.
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

package workqueue

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type WorkQueue struct {
	queue workqueue.TypedRateLimitingInterface[any]
}

type WorkItem struct {
	Object   any
	Callback func(obj any) error
}

func DefaultControllerRateLimiter() workqueue.TypedRateLimiter[any] {
	return workqueue.DefaultTypedControllerRateLimiter[any]()
}

func New(r workqueue.TypedRateLimiter[any]) *WorkQueue {
	queue := workqueue.NewTypedRateLimitingQueue(r)
	return &WorkQueue{queue}
}

func (q *WorkQueue) Run(done <-chan struct{}) {
	go func() {
		<-done
		q.queue.ShutDown()
	}()
	for {
		select {
		case <-done:
			return
		default:
			q.processNextWorkItem()
		}
	}
}

func (q *WorkQueue) Enqueue(obj any, callback func(obj any) error) {
	runtimeObj, ok := obj.(runtime.Object)
	if !ok {
		klog.Warningf("unexpected object type %T: runtime.Object required", obj)
		return
	}

	workItem := &WorkItem{
		Object:   runtimeObj.DeepCopyObject(),
		Callback: callback,
	}

	q.queue.AddRateLimited(workItem)
}

func (q *WorkQueue) processNextWorkItem() {
	item, shutdown := q.queue.Get()
	if shutdown {
		return
	}
	defer q.queue.Done(item)

	workItem, ok := item.(*WorkItem)
	if !ok {
		klog.Errorf("Unexpected item in queue: %v", item)
		return
	}

	err := q.reconcile(workItem)
	if err != nil {
		klog.Errorf("Failed to reconcile work item %v: %v", workItem.Object, err)
		q.queue.AddRateLimited(workItem)
	} else {
		q.queue.Forget(workItem)
	}
}

func (q *WorkQueue) reconcile(workItem *WorkItem) error {
	if workItem.Callback == nil {
		return fmt.Errorf("no callback to process work item: %+v", workItem)
	}
	return workItem.Callback(workItem.Object)
}
