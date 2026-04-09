package queue

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

func makeApp() *appsv1alpha1.Application {
	return &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-app",
		},
	}
}

// helper to pop with timeout to avoid blocking tests indefinitely
func popWithTimeout(q SchedulingQueue, timeout time.Duration) (*appsv1alpha1.Application, error) {
	ch := make(chan struct{})
	var app *appsv1alpha1.Application
	var err error
	go func() {
		app, err = q.Pop()
		close(ch)
	}()

	select {
	case <-ch:
		return app, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for Pop")
	}
}

func TestSchedulingQueue_AddAndPop(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}
	if popped.Name != app.Name || popped.Namespace != app.Namespace {
		t.Errorf("popped app mismatch: got %s/%s, want %s/%s", popped.Namespace, popped.Name, app.Namespace, app.Name)
	}
	q.Done(popped)
}

func TestSchedulingQueue_DuplicateAdd(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)
	q.Add(app) // duplicate add should be ignored

	if q.Len() != 1 {
		t.Errorf("expected queue length 1, got %d", q.Len())
	}

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}
	q.Done(popped)
}

func TestSchedulingQueue_Requeue(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}

	q.Requeue(popped)
	q.Done(popped)

	// Requeue uses rate limiting; try popping with retries
	var got *appsv1alpha1.Application
	for i := 0; i < 10; i++ {
		p, err := popWithTimeout(q, 200*time.Millisecond)
		if err == nil {
			got = p
			break
		}
		// allow rate limiter/backoff to elapse
		time.Sleep(100 * time.Millisecond)
	}
	if got == nil {
		t.Fatalf("expected item to be requeued, but did not receive it")
	}
	q.Done(got)
}

func TestSchedulingQueue_ForgetClearsRateLimiter(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}

	// simulate a retry
	q.Requeue(popped)
	q.Done(popped)

	// Forget should clear rate limiter state and remove from activeQ so Add() can re-add
	q.Forget(popped)

	q.Add(app)
	if q.Len() != 1 {
		t.Fatalf("expected queue length 1 after Forget and Add, got %d", q.Len())
	}

	p, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}
	q.Done(p)
}

func TestSchedulingQueue_Delete(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)
	q.Delete(app)

	popped, err := q.Pop()
	if err == nil {
		// Since item was deleted from activeQ, Pop should return an error
		t.Fatalf("expected error when popping deleted item, got app %v", popped)
	}
}

func TestSchedulingQueue_RequeueAfter(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	app := makeApp()
	q.Add(app)

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}

	q.RequeueAfter(popped, 100*time.Millisecond)
	q.Done(popped)

	// Now wait for the item to reappear
	p, err := popWithTimeout(q, 2*time.Second)
	if err != nil {
		t.Fatalf("expected item to be requeued, but Pop timed out: %v", err)
	}
	q.Done(p)
}

func TestSchedulingQueue_Update(t *testing.T) {
	q := NewSchedulingQueue()
	defer q.Close()

	oldApp := makeApp()
	q.Add(oldApp)

	newApp := makeApp()
	newApp.Labels = map[string]string{"updated": "true"}

	q.Update(oldApp, newApp)

	popped, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}
	if popped.Labels == nil || popped.Labels["updated"] != "true" {
		t.Errorf("expected updated app, got old app")
	}
	q.Done(popped)
}
