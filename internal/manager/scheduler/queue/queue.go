package queue

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
)

// Default retry configuration
const (
	// DefaultUnschedulableDelay is the default delay for unschedulable applications
	DefaultUnschedulableDelay = 5 * time.Minute
)

type schedulingQueue struct {
	queue workqueue.RateLimitingInterface
	// activeQ stores applications that are currently in the queue or being processed.
	// This is used to prevent duplicate additions and to store extra info.
	activeQ map[string]*QueuedApplicationInfo
	mx      sync.RWMutex
}

func NewSchedulingQueue() SchedulingQueue {
	return &schedulingQueue{
		queue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "scheduler"),
		activeQ: make(map[string]*QueuedApplicationInfo),
	}
}

func (q *schedulingQueue) Add(app *appsv1alpha1.Application) {
	q.mx.Lock()
	defer q.mx.Unlock()

	key := appKey(app)
	if _, exists := q.activeQ[key]; !exists {
		q.activeQ[key] = &QueuedApplicationInfo{
			Application: app,
			Attempts:    0,
		}
		q.queue.Add(key)
	}
}

func (q *schedulingQueue) AddUnschedulableIfNotPresent(app *appsv1alpha1.Application, pInfo *QueuedApplicationInfo) error {
	q.mx.Lock()
	defer q.mx.Unlock()

	key := appKey(app)
	if _, exists := q.activeQ[key]; !exists {
		if pInfo != nil {
			pInfo.Attempts++
		}
		q.activeQ[key] = pInfo
		q.queue.AddRateLimited(key)
	}
	return nil
}

func (q *schedulingQueue) Pop() (*appsv1alpha1.Application, error) {
	item, shutdown := q.queue.Get()
	if shutdown {
		return nil, fmt.Errorf("queue is shut down")
	}

	key, ok := item.(string)
	if !ok {
		q.queue.Done(item)
		return nil, fmt.Errorf("expected string key, got %T", item)
	}

	q.mx.RLock()
	pInfo, exists := q.activeQ[key]
	q.mx.RUnlock()

	if !exists || pInfo == nil || pInfo.Application == nil {
		q.queue.Done(item)
		return nil, fmt.Errorf("application not found in activeQ: %s", key)
	}

	// Note: We do NOT call Done() here. The caller must call Done() when finished processing.
	return pInfo.Application, nil
}

// Done marks an item as finished processing. Must be called after Pop().
func (q *schedulingQueue) Done(app *appsv1alpha1.Application) {
	key := appKey(app)
	q.queue.Done(key)
}

// Forget indicates that an item is finished being retried. Clears rate limiter history.
func (q *schedulingQueue) Forget(app *appsv1alpha1.Application) {
	key := appKey(app)
	q.queue.Forget(key)

	q.mx.Lock()
	delete(q.activeQ, key)
	q.mx.Unlock()
}

// Requeue adds the application back to the queue with rate limiting for retry.
func (q *schedulingQueue) Requeue(app *appsv1alpha1.Application) {
	key := appKey(app)
	q.queue.AddRateLimited(key)
}

// RequeueAfter adds the application back to the queue after a fixed delay.
func (q *schedulingQueue) RequeueAfter(app *appsv1alpha1.Application, delay interface{}) {
	key := appKey(app)
	d, ok := delay.(time.Duration)
	if !ok {
		d = DefaultUnschedulableDelay
	}
	q.queue.AddAfter(key, d)
}

func (q *schedulingQueue) Update(oldApp, newApp *appsv1alpha1.Application) {
	q.mx.Lock()
	defer q.mx.Unlock()

	oldKey := appKey(oldApp)
	newKey := appKey(newApp)

	// If key changed, remove old entry
	if oldKey != newKey {
		delete(q.activeQ, oldKey)
	}

	// Update or add new entry
	if pInfo, exists := q.activeQ[newKey]; exists {
		pInfo.Application = newApp
	} else {
		q.activeQ[newKey] = &QueuedApplicationInfo{
			Application: newApp,
			Attempts:    0,
		}
		q.queue.Add(newKey)
	}
}

func (q *schedulingQueue) Delete(app *appsv1alpha1.Application) {
	q.mx.Lock()
	defer q.mx.Unlock()

	key := appKey(app)
	delete(q.activeQ, key)
	// Note: We cannot remove from workqueue directly, but deleted items
	// will be detected and skipped when popped (not found in activeQ).
}

// Len returns the current length of the queue.
func (q *schedulingQueue) Len() int {
	return q.queue.Len()
}

func (q *schedulingQueue) Run() {
	// No-op for simple workqueue wrapper
}

func (q *schedulingQueue) Close() {
	q.queue.ShutDown()
}

func appKey(app *appsv1alpha1.Application) string {
	return fmt.Sprintf("%s/%s", app.Namespace, app.Name)
}
