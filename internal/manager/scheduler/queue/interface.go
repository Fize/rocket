package queue

import (
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

// SchedulingQueue stores applications that need to be scheduled.
type SchedulingQueue interface {
	// Add adds an application to the queue for scheduling.
	Add(app *appsv1alpha1.Application)
	// AddUnschedulableIfNotPresent adds an unschedulable application back to the queue with rate limiting.
	AddUnschedulableIfNotPresent(app *appsv1alpha1.Application, pInfo *QueuedApplicationInfo) error
	// Pop removes the head of the queue and returns it. It blocks if the queue is empty.
	// The caller MUST call Done() when finished processing the item.
	Pop() (*appsv1alpha1.Application, error)
	// Done marks an item as finished processing. Must be called after Pop().
	Done(app *appsv1alpha1.Application)
	// Forget indicates that an item is finished being retried. This clears the rate limiter history.
	Forget(app *appsv1alpha1.Application)
	// Requeue adds the application back to the queue with rate limiting for retry.
	Requeue(app *appsv1alpha1.Application)
	// RequeueAfter adds the application back to the queue after a fixed delay.
	RequeueAfter(app *appsv1alpha1.Application, delay interface{})
	// Update updates an application in the queue.
	Update(oldApp, newApp *appsv1alpha1.Application)
	// Delete deletes an application from the queue.
	Delete(app *appsv1alpha1.Application)
	// Len returns the current length of the queue.
	Len() int
	// Run starts the queue.
	Run()
	// Close closes the queue.
	Close()
}

// QueuedApplicationInfo is a wrapper around Application with extra info.
type QueuedApplicationInfo struct {
	Application *appsv1alpha1.Application
	// Attempts is the number of scheduling attempts.
	Attempts int
}
