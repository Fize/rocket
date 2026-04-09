package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedcache "github.com/fize/rocket/internal/manager/scheduler/cache"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/fize/rocket/internal/manager/scheduler/queue"
)

// Test fakes

type fakeCache struct {
	addCalls    int
	updateCalls int
	removeCalls int

	addErr error

	removedNames []string
}

func (c *fakeCache) AddCluster(cluster *clusterv1alpha1.ManagedCluster) error {
	c.addCalls++
	if c.addErr != nil {
		return c.addErr
	}
	return nil
}

func (c *fakeCache) UpdateCluster(oldCluster, newCluster *clusterv1alpha1.ManagedCluster) error {
	c.updateCalls++
	return nil
}

func (c *fakeCache) RemoveCluster(cluster *clusterv1alpha1.ManagedCluster) error {
	c.removeCalls++
	if cluster != nil {
		c.removedNames = append(c.removedNames, cluster.Name)
	}
	return nil
}

func (c *fakeCache) Snapshot() *schedcache.Snapshot {
	return &schedcache.Snapshot{Clusters: map[string]*schedcache.ClusterInfo{}}
}

func (c *fakeCache) AssumeApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error {
	return nil
}

func (c *fakeCache) ForgetApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error {
	return nil
}

func (c *fakeCache) RemoveAssumedApplication(appKey string) {}

type fakeQueue struct {
	adds    []types.NamespacedName
	deletes []types.NamespacedName
}

func (q *fakeQueue) Add(app *appsv1alpha1.Application) {
	q.adds = append(q.adds, types.NamespacedName{Namespace: app.Namespace, Name: app.Name})
}

func (q *fakeQueue) AddUnschedulableIfNotPresent(app *appsv1alpha1.Application, pInfo *queue.QueuedApplicationInfo) error {
	return nil
}

func (q *fakeQueue) Pop() (*appsv1alpha1.Application, error) { return nil, fmt.Errorf("not used") }

func (q *fakeQueue) Done(app *appsv1alpha1.Application)                            {}
func (q *fakeQueue) Forget(app *appsv1alpha1.Application)                          {}
func (q *fakeQueue) Requeue(app *appsv1alpha1.Application)                         {}
func (q *fakeQueue) RequeueAfter(app *appsv1alpha1.Application, delay interface{}) {}
func (q *fakeQueue) Update(oldApp, newApp *appsv1alpha1.Application)               {}

func (q *fakeQueue) Delete(app *appsv1alpha1.Application) {
	q.deletes = append(q.deletes, types.NamespacedName{Namespace: app.Namespace, Name: app.Name})
}

func (q *fakeQueue) Len() int { return 0 }
func (q *fakeQueue) Run()     {}
func (q *fakeQueue) Close()   {}

// Tests

func TestNeedsScheduling(t *testing.T) {
	base := &appsv1alpha1.Application{}

	app1 := base.DeepCopy()
	app1.Status.SchedulingPhase = ""
	if !needsScheduling(app1) {
		t.Fatalf("expected pending when phase empty and no placement")
	}

	app2 := base.DeepCopy()
	app2.Status.SchedulingPhase = appsv1alpha1.Pending
	if !needsScheduling(app2) {
		t.Fatalf("expected pending when phase Pending and no placement")
	}

	app3 := base.DeepCopy()
	app3.Status.SchedulingPhase = appsv1alpha1.Scheduled
	if needsScheduling(app3) {
		t.Fatalf("expected not pending when phase Scheduled")
	}

	app4 := base.DeepCopy()
	app4.Status.SchedulingPhase = appsv1alpha1.Pending
	app4.Status.Placement.Topology = []appsv1alpha1.ClusterTopology{{Name: "c1", Replicas: 1}}
	if needsScheduling(app4) {
		t.Fatalf("expected not pending when placement already set")
	}

	// Test Scaling Logic
	app5 := base.DeepCopy()
	app5.Status.SchedulingPhase = appsv1alpha1.Scheduled
	app5.Spec.Replicas = ptrInt32(5)
	app5.Status.Placement.Topology = []appsv1alpha1.ClusterTopology{{Name: "c1", Replicas: 3}}
	if !needsScheduling(app5) {
		t.Fatalf("expected scheduling needed when replicas differ (5 desired vs 3 scheduled)")
	}

	app6 := base.DeepCopy()
	app6.Status.SchedulingPhase = appsv1alpha1.Scheduled
	app6.Spec.Replicas = ptrInt32(5)
	app6.Status.Placement.Topology = []appsv1alpha1.ClusterTopology{{Name: "c1", Replicas: 3}, {Name: "c2", Replicas: 2}}
	if needsScheduling(app6) {
		t.Fatalf("expected not scheduling needed when replicas match (5 vs 3+2)")
	}
}

func TestAppReconciler_Reconcile_AddsPendingToQueue(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	app := &appsv1alpha1.Application{}
	app.Namespace = "default"
	app.Name = "app1"
	app.Spec.Replicas = ptrInt32(1)
	app.Status.SchedulingPhase = appsv1alpha1.Pending

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	q := &fakeQueue{}
	fc := &fakeCache{}

	r := &AppReconciler{Client: c, Queue: q, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "app1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(q.adds) != 1 || q.adds[0].Name != "app1" {
		t.Fatalf("expected app enqueued once, got %+v", q.adds)
	}
}

func TestAppReconciler_Reconcile_DeleteNotFoundFromQueue(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	q := &fakeQueue{}
	fc := &fakeCache{}

	r := &AppReconciler{Client: c, Queue: q, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "missing"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(q.deletes) != 1 || q.deletes[0].Name != "missing" {
		t.Fatalf("expected delete called once, got %+v", q.deletes)
	}
}

func TestClusterReconciler_Reconcile_AddOrUpdateCache(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	cluster := &clusterv1alpha1.ManagedCluster{}
	cluster.Name = "c1"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	fc := &fakeCache{}

	r := &ClusterReconciler{Client: c, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "c1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if fc.addCalls != 1 {
		t.Fatalf("expected AddCluster called once, got %d", fc.addCalls)
	}
	if fc.updateCalls != 0 {
		t.Fatalf("expected UpdateCluster not called, got %d", fc.updateCalls)
	}
}

func TestClusterReconciler_Reconcile_UpdateWhenAddFails(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	cluster := &clusterv1alpha1.ManagedCluster{}
	cluster.Name = "c1"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	fc := &fakeCache{addErr: fmt.Errorf("already exists")}

	r := &ClusterReconciler{Client: c, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "c1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if fc.addCalls != 1 {
		t.Fatalf("expected AddCluster called once, got %d", fc.addCalls)
	}
	if fc.updateCalls != 1 {
		t.Fatalf("expected UpdateCluster called once, got %d", fc.updateCalls)
	}
}

func TestClusterReconciler_Reconcile_RemovesDeletedCluster(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	ts := metav1.NewTime(time.Now())
	cluster := &clusterv1alpha1.ManagedCluster{}
	cluster.Name = "c1"
	cluster.DeletionTimestamp = &ts
	cluster.Finalizers = []string{"test-finalizer"} // Add finalizer so fake client accepts it

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	fc := &fakeCache{}

	r := &ClusterReconciler{Client: c, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "c1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if fc.removeCalls != 1 {
		t.Fatalf("expected RemoveCluster called once, got %d", fc.removeCalls)
	}
	if len(fc.removedNames) != 1 || fc.removedNames[0] != "c1" {
		t.Fatalf("expected removed c1, got %+v", fc.removedNames)
	}
}

func TestClusterReconciler_Reconcile_RemovesMissingCluster(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	fc := &fakeCache{}

	r := &ClusterReconciler{Client: c, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if fc.removeCalls != 1 {
		t.Fatalf("expected RemoveCluster called once, got %d", fc.removeCalls)
	}
}

func TestClusterReconciler_Reconcile_RemovesMissingCluster_NoRemovedName(t *testing.T) {
	// Extra assertion: missing cluster should still call RemoveCluster even if no object existed.
	ctx := context.Background()
	scheme := testScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	fc := &fakeCache{}

	r := &ClusterReconciler{Client: c, Cache: fc}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if fc.removeCalls != 1 {
		t.Fatalf("expected RemoveCluster called once, got %d", fc.removeCalls)
	}
	if len(fc.removedNames) != 1 || fc.removedNames[0] != "missing" {
		// In the not-found path, reconciler passes a dummy object with Name set.
		t.Fatalf("expected removedNames contains 'missing', got %+v", fc.removedNames)
	}
}
