package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedcache "github.com/hex-techs/rocket/internal/manager/scheduler/cache"
	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	"github.com/hex-techs/rocket/internal/manager/scheduler/queue"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

// Test helpers

func ptrInt32(v int32) *int32 { return &v }

func testScheme(t *testing.T) *k8sruntime.Scheme {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := clusterv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add cluster scheme: %v", err)
	}
	return scheme
}

// testQueue is a fake implementation of SchedulingQueue for testing
type testQueue struct {
	popItems []*appsv1alpha1.Application
	popIdx   int
	popErr   error
}

func (q *testQueue) Add(app *appsv1alpha1.Application) {}

func (q *testQueue) AddUnschedulableIfNotPresent(app *appsv1alpha1.Application, pInfo *queue.QueuedApplicationInfo) error {
	return nil
}

func (q *testQueue) Pop() (*appsv1alpha1.Application, error) {
	if q.popErr != nil {
		return nil, q.popErr
	}
	if q.popIdx >= len(q.popItems) {
		return nil, fmt.Errorf("empty queue")
	}
	item := q.popItems[q.popIdx]
	q.popIdx++
	return item, nil
}

func (q *testQueue) Done(app *appsv1alpha1.Application)                            {}
func (q *testQueue) Forget(app *appsv1alpha1.Application)                          {}
func (q *testQueue) Requeue(app *appsv1alpha1.Application)                         {}
func (q *testQueue) RequeueAfter(app *appsv1alpha1.Application, delay interface{}) {}
func (q *testQueue) Update(oldApp, newApp *appsv1alpha1.Application)               {}
func (q *testQueue) Delete(app *appsv1alpha1.Application)                          {}
func (q *testQueue) Len() int                                                      { return len(q.popItems) - q.popIdx }
func (q *testQueue) Run()                                                          {}
func (q *testQueue) Close()                                                        {}

// Tests

func TestScheduler_selectCluster_TieBreaksDeterministically(t *testing.T) {
	s := &Scheduler{}

	selected := s.selectCluster(map[string]int64{
		"b": 1,
		"a": 1,
	})

	if selected != "a" {
		t.Fatalf("expected a, got %q", selected)
	}
}

// Merged from spread_test.go: tests for selectClustersSpread
func TestSelectClustersSpread(t *testing.T) {
	config := &framework.SchedulerConfig{
		Strategy: framework.StrategySpread,
		SpreadConstraints: &framework.SpreadConstraints{
			MaxClusters: 3,
			MinReplicas: 1,
		},
	}

	scheduler := &Scheduler{
		config: config,
	}

	tests := []struct {
		name            string
		app             *appsv1alpha1.Application
		scores          map[string]int64
		expectedCount   int
		expectedTotal   int32
		minReplicasEach int32
	}{
		{
			name: "distribute 10 replicas across 3 clusters",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: ptrInt32(10),
				},
			},
			scores: map[string]int64{
				"cluster-a": 100,
				"cluster-b": 80,
				"cluster-c": 60,
				"cluster-d": 40,
			},
			expectedCount:   3,
			expectedTotal:   10,
			minReplicasEach: 1,
		},
		{
			name: "distribute 5 replicas with minReplicas=2",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: ptrInt32(5),
				},
			},
			scores: map[string]int64{
				"cluster-a": 100,
				"cluster-b": 80,
				"cluster-c": 60,
			},
			expectedCount:   2, // Can only use 2 clusters with minReplicas=2
			expectedTotal:   5,
			minReplicasEach: 2,
		},
		{
			name: "single replica",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: ptrInt32(1),
				},
			},
			scores: map[string]int64{
				"cluster-a": 100,
				"cluster-b": 80,
			},
			expectedCount:   1,
			expectedTotal:   1,
			minReplicasEach: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler.config.SpreadConstraints.MinReplicas = tt.minReplicasEach

			// Pass nil snapshot as we are using default Score weight mode
			placement := scheduler.selectClustersSpread(tt.app, tt.scores, nil)

			if len(placement) != tt.expectedCount {
				t.Errorf("Expected %d clusters, got %d", tt.expectedCount, len(placement))
			}

			var totalReplicas int32 = 0
			for _, p := range placement {
				totalReplicas += p.Replicas
				if p.Replicas < tt.minReplicasEach {
					t.Errorf("Cluster %s has %d replicas, less than minimum %d",
						p.Name, p.Replicas, tt.minReplicasEach)
				}
			}

			if totalReplicas != tt.expectedTotal {
				t.Errorf("Expected total %d replicas, got %d", tt.expectedTotal, totalReplicas)
			}
		})
	}
}

func TestScheduler_bind_DefaultsReplicasToOne(t *testing.T) {
	ctx := context.Background()
	scheme := testScheme(t)

	app := &appsv1alpha1.Application{}
	app.Namespace = "default"
	app.Name = "app1"
	app.Spec.Replicas = nil

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()

	s := &Scheduler{client: c}

	placement := []appsv1alpha1.ClusterTopology{{Name: "a", Replicas: 1}}
	if err := s.bind(ctx, app, placement); err != nil {
		t.Fatalf("bind: %v", err)
	}

	got := &appsv1alpha1.Application{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatalf("get app: %v", err)
	}

	if got.Status.SchedulingPhase != appsv1alpha1.Scheduled {
		t.Fatalf("expected scheduling phase %q, got %q", appsv1alpha1.Scheduled, got.Status.SchedulingPhase)
	}
	if len(got.Status.Placement.Topology) != 1 {
		t.Fatalf("expected 1 topology entry, got %d", len(got.Status.Placement.Topology))
	}
	if got.Status.Placement.Topology[0].Name != "a" {
		t.Fatalf("expected selected cluster 'a', got %q", got.Status.Placement.Topology[0].Name)
	}
	if got.Status.Placement.Topology[0].Replicas != 1 {
		t.Fatalf("expected replicas 1, got %d", got.Status.Placement.Topology[0].Replicas)
	}
}

func TestScheduler_scheduleOne_BindsApplicationStatus(t *testing.T) {
	scheme := testScheme(t)

	app := &appsv1alpha1.Application{}
	app.Namespace = "default"
	app.Name = "app1"
	app.Spec.Replicas = ptrInt32(2)
	app.Status.SchedulingPhase = appsv1alpha1.Pending

	clA := &clusterv1alpha1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "a"}}
	clB := &clusterv1alpha1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "b"}}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app, clA, clB).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()

	cc := schedcache.NewCache()
	if err := cc.AddCluster(clA); err != nil {
		t.Fatalf("add cluster a: %v", err)
	}
	if err := cc.AddCluster(clB); err != nil {
		t.Fatalf("add cluster b: %v", err)
	}

	// Create a snapshot directly since we don't want to call scheduleOne (which pops from queue)
	snapshot := cc.Snapshot()

	// Use scheduler's selectCluster and bind directly
	s := &Scheduler{
		client:    c,
		cache:     cc,
		framework: NewScheduler(c, cc, &testQueue{}).framework,
	}

	// Manually run filter and score
	state := framework.NewCycleState()
	feasibleClusters := make([]*clusterv1alpha1.ManagedCluster, 0)
	for _, clusterInfo := range snapshot.Clusters {
		cluster := clusterInfo.Cluster
		status := s.framework.RunFilterPlugins(context.Background(), state, app, cluster)
		if status.IsSuccess() {
			feasibleClusters = append(feasibleClusters, cluster)
		}
	}

	if len(feasibleClusters) == 0 {
		t.Fatal("expected at least one feasible cluster")
	}

	// Score
	scores, status := s.framework.RunScorePlugins(context.Background(), state, app, feasibleClusters)
	if !status.IsSuccess() {
		t.Fatalf("score failed: %s", status.Message)
	}

	// Select
	selectedCluster := s.selectCluster(scores)

	// Bind
	ctx := context.Background()
	placement := []appsv1alpha1.ClusterTopology{{Name: selectedCluster, Replicas: *app.Spec.Replicas}}
	if err := s.bind(ctx, app, placement); err != nil {
		t.Fatalf("bind: %v", err)
	}

	got := &appsv1alpha1.Application{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatalf("get app: %v", err)
	}

	if got.Status.SchedulingPhase != appsv1alpha1.Scheduled {
		t.Fatalf("expected scheduling phase %q, got %q", appsv1alpha1.Scheduled, got.Status.SchedulingPhase)
	}
	if len(got.Status.Placement.Topology) != 1 {
		t.Fatalf("expected 1 topology entry, got %d", len(got.Status.Placement.Topology))
	}
	if got.Status.Placement.Topology[0].Name != "a" {
		t.Fatalf("expected selected cluster 'a', got %q", got.Status.Placement.Topology[0].Name)
	}
	if got.Status.Placement.Topology[0].Replicas != 2 {
		t.Fatalf("expected replicas 2, got %d", got.Status.Placement.Topology[0].Replicas)
	}
}

func TestScheduler_scheduleOne_NoClusters_DoesNotBind(t *testing.T) {
	scheme := testScheme(t)

	app := &appsv1alpha1.Application{}
	app.Namespace = "default"
	app.Name = "app1"
	app.Spec.Replicas = ptrInt32(1)
	app.Status.SchedulingPhase = appsv1alpha1.Pending

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()

	cc := schedcache.NewCache() // empty
	snapshot := cc.Snapshot()

	// Verify that snapshot has no clusters
	if len(snapshot.Clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(snapshot.Clusters))
	}

	// Since there are no clusters, bind should not be called
	// The app status should remain Pending
	ctx := context.Background()
	got := &appsv1alpha1.Application{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatalf("get app: %v", err)
	}

	if got.Status.SchedulingPhase != appsv1alpha1.Pending {
		t.Fatalf("expected scheduling phase %q, got %q", appsv1alpha1.Pending, got.Status.SchedulingPhase)
	}
	if len(got.Status.Placement.Topology) != 0 {
		t.Fatalf("expected no placement, got %v", got.Status.Placement.Topology)
	}
}

func TestScheduler_scheduleOne_AllClustersFiltered_DoesNotBind(t *testing.T) {
	scheme := testScheme(t)

	app := &appsv1alpha1.Application{}
	app.Namespace = "default"
	app.Name = "app1"
	app.Spec.Replicas = ptrInt32(1)
	app.Status.SchedulingPhase = appsv1alpha1.Pending
	template := v1.PodTemplateSpec{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "c"}}}}
	templateBytes, _ := json.Marshal(template)
	app.Spec.Template = k8sruntime.RawExtension{Raw: templateBytes}

	clA := &clusterv1alpha1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "a"}}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app, clA).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()

	cc := schedcache.NewCache()
	if err := cc.AddCluster(clA); err != nil {
		t.Fatalf("add cluster a: %v", err)
	}

	s := &Scheduler{
		client:    c,
		cache:     cc,
		framework: NewScheduler(c, cc, &testQueue{}).framework,
	}

	// Run filter to verify cluster is filtered out (resource plugin filters if no ResourceSummary)
	snapshot := cc.Snapshot()
	state := framework.NewCycleState()
	feasibleClusters := make([]*clusterv1alpha1.ManagedCluster, 0)
	for _, clusterInfo := range snapshot.Clusters {
		cluster := clusterInfo.Cluster
		status := s.framework.RunFilterPlugins(context.Background(), state, app, cluster)
		if status.IsSuccess() {
			feasibleClusters = append(feasibleClusters, cluster)
		}
	}

	// Verify all clusters were filtered out
	if len(feasibleClusters) != 0 {
		t.Fatalf("expected 0 feasible clusters, got %d", len(feasibleClusters))
	}

	// App status should remain Pending since no binding happened
	ctx := context.Background()
	got := &appsv1alpha1.Application{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatalf("get app: %v", err)
	}

	if got.Status.SchedulingPhase != appsv1alpha1.Pending {
		t.Fatalf("expected scheduling phase %q, got %q", appsv1alpha1.Pending, got.Status.SchedulingPhase)
	}
	if len(got.Status.Placement.Topology) != 0 {
		t.Fatalf("expected no placement, got %v", got.Status.Placement.Topology)
	}
}

func TestScheduler_gatherExistingPlacements(t *testing.T) {
	scheme := testScheme(t)

	// Create some applications with placements
	app1 := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "default"},
		Status: appsv1alpha1.ApplicationStatus{
			SchedulingPhase: appsv1alpha1.Scheduled,
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{
					{Name: "cluster-a", Replicas: 3},
					{Name: "cluster-b", Replicas: 2},
				},
			},
		},
	}
	app2 := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "app2", Namespace: "default"},
		Status: appsv1alpha1.ApplicationStatus{
			SchedulingPhase: appsv1alpha1.Scheduled,
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{
					{Name: "cluster-a", Replicas: 5},
				},
			},
		},
	}
	// Pending app should not be included
	app3 := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "app3", Namespace: "default"},
		Status: appsv1alpha1.ApplicationStatus{
			SchedulingPhase: appsv1alpha1.Pending,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app1, app2, app3).
		Build()

	s := &Scheduler{client: c}
	snapshot := &schedcache.Snapshot{
		Clusters: make(map[string]*schedcache.ClusterInfo),
	}

	placements := s.gatherExistingPlacements(context.Background(), snapshot)

	// Should have 3 placements total (2 from app1, 1 from app2)
	if len(placements) != 3 {
		t.Fatalf("expected 3 placements, got %d: %v", len(placements), placements)
	}

	// Verify placement content
	clusterCounts := make(map[string]int32)
	for _, p := range placements {
		clusterCounts[p.Name] += p.Replicas
	}

	if clusterCounts["cluster-a"] != 8 { // 3 + 5
		t.Errorf("cluster-a total replicas = %d, want 8", clusterCounts["cluster-a"])
	}
	if clusterCounts["cluster-b"] != 2 {
		t.Errorf("cluster-b total replicas = %d, want 2", clusterCounts["cluster-b"])
	}
}

func TestGatherExistingPlacementsWithAssumed(t *testing.T) {
	scheme := k8sruntime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)

	// Committed app
	app1 := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "default"},
		Status: appsv1alpha1.ApplicationStatus{
			SchedulingPhase: appsv1alpha1.Scheduled,
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{
					{Name: "cluster-a", Replicas: 3},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app1).
		Build()

	s := &Scheduler{client: c}

	// Snapshot with assumed app
	snapshot := &schedcache.Snapshot{
		Clusters: map[string]*schedcache.ClusterInfo{
			"cluster-a": {
				AssumedApplications: map[string]schedcache.AssumedApplicationInfo{
					"default/app2": {Replicas: 5},
					"default/app1": {Replicas: 10}, // Should be ignored as it's already committed
				},
			},
			"cluster-b": {
				AssumedApplications: map[string]schedcache.AssumedApplicationInfo{
					"default/app2": {Replicas: 2},
				},
			},
		},
	}

	placements := s.gatherExistingPlacements(context.Background(), snapshot)

	// Should have 3 placements total:
	// 1. app1 on cluster-a (3 replicas) - from API
	// 2. app2 on cluster-a (5 replicas) - from Cache
	// 3. app2 on cluster-b (2 replicas) - from Cache
	if len(placements) != 3 {
		t.Fatalf("expected 3 placements, got %d: %v", len(placements), placements)
	}

	clusterCounts := make(map[string]int32)
	for _, p := range placements {
		clusterCounts[p.Name] += p.Replicas
	}

	if clusterCounts["cluster-a"] != 8 { // 3 + 5
		t.Errorf("cluster-a total replicas = %d, want 8", clusterCounts["cluster-a"])
	}
	if clusterCounts["cluster-b"] != 2 {
		t.Errorf("cluster-b total replicas = %d, want 2", clusterCounts["cluster-b"])
	}
}

func TestScheduler_getTopologyKey(t *testing.T) {
	tests := []struct {
		name     string
		config   *framework.SchedulerConfig
		expected string
	}{
		{
			name:     "nil config uses default",
			config:   nil,
			expected: DefaultTopologyKey,
		},
		{
			name: "no topology plugin uses default",
			config: &framework.SchedulerConfig{
				ScorePlugins: []framework.PluginConfig{
					{Name: "Affinity"},
				},
			},
			expected: DefaultTopologyKey,
		},
		{
			name: "topology plugin with custom key",
			config: &framework.SchedulerConfig{
				ScorePlugins: []framework.PluginConfig{
					{Name: "TopologySpread", Args: map[string]interface{}{"topologyKey": "custom/zone"}},
				},
			},
			expected: "custom/zone",
		},
		{
			name: "topology plugin with empty key uses default",
			config: &framework.SchedulerConfig{
				ScorePlugins: []framework.PluginConfig{
					{Name: "TopologySpread", Args: map[string]interface{}{"topologyKey": ""}},
				},
			},
			expected: DefaultTopologyKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scheduler{config: tt.config}
			got := s.getTopologyKey()
			if got != tt.expected {
				t.Errorf("getTopologyKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestScheduler_prefilterByAffinity(t *testing.T) {
	// Create a snapshot with clusters
	cc := schedcache.NewCache()
	clusterA := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-a",
			Labels: map[string]string{"region": "us-east", "env": "prod"},
		},
	}
	clusterB := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-b",
			Labels: map[string]string{"region": "us-west", "env": "prod"},
		},
	}
	clusterC := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-c",
			Labels: map[string]string{"region": "eu-west", "env": "staging"},
		},
	}

	cc.AddCluster(clusterA)
	cc.AddCluster(clusterB)
	cc.AddCluster(clusterC)

	s := &Scheduler{}

	tests := []struct {
		name          string
		affinity      *v1.NodeAffinity
		expectedNames []string
	}{
		{
			name:          "no affinity returns all clusters",
			affinity:      nil,
			expectedNames: []string{"cluster-a", "cluster-b", "cluster-c"},
		},
		{
			name: "In operator filters by label value",
			affinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{Key: "region", Operator: v1.NodeSelectorOpIn, Values: []string{"us-east"}},
							},
						},
					},
				},
			},
			expectedNames: []string{"cluster-a"},
		},
		{
			name: "In operator with multiple values",
			affinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{Key: "region", Operator: v1.NodeSelectorOpIn, Values: []string{"us-east", "us-west"}},
							},
						},
					},
				},
			},
			expectedNames: []string{"cluster-a", "cluster-b"},
		},
		{
			name: "AND semantics within term",
			affinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{Key: "region", Operator: v1.NodeSelectorOpIn, Values: []string{"us-east", "us-west"}},
								{Key: "env", Operator: v1.NodeSelectorOpIn, Values: []string{"prod"}},
							},
						},
					},
				},
			},
			expectedNames: []string{"cluster-a", "cluster-b"},
		},
		{
			name: "no matching clusters",
			affinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{Key: "region", Operator: v1.NodeSelectorOpIn, Values: []string{"ap-south"}},
							},
						},
					},
				},
			},
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: tt.affinity,
				},
			}

			snapshot := cc.Snapshot()
			candidates := s.prefilterByAffinity(app, snapshot)

			gotNames := make(map[string]bool)
			for _, c := range candidates {
				gotNames[c.Cluster.Name] = true
			}

			if len(candidates) != len(tt.expectedNames) {
				t.Errorf("got %d candidates, want %d", len(candidates), len(tt.expectedNames))
			}

			for _, name := range tt.expectedNames {
				if !gotNames[name] {
					t.Errorf("expected cluster %s not found in candidates", name)
				}
			}
		})
	}
}

func TestScheduler_SelectClustersStatefulSetWaterfill(t *testing.T) {
	s := &Scheduler{}

	// Helper to create cluster with capacity (via ResourceSummary)
	createCluster := func(name string, cpuCap string) *clusterv1alpha1.ManagedCluster {
		qty := resource.MustParse(cpuCap)
		return &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status: clusterv1alpha1.ManagedClusterStatus{
				ResourceSummary: []clusterv1alpha1.ResourceSummary{
					{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU: qty,
						},
						Allocated: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("0"),
						},
					},
				},
			},
		}
	}

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "sts-app", Namespace: "default"},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{Kind: "StatefulSet"},
			// Standard pod template with 1 CPU request
			Template: k8sruntime.RawExtension{Raw: []byte(`{"spec":{"containers":[{"name":"c","resources":{"requests":{"cpu":"1"}}}]}}`)},
		},
	}

	tests := []struct {
		name              string
		scores            map[string]int64
		existingPlacement map[string]int32
		totalReplicas     int32
		clusters          map[string]string // Name -> CPU Allocatable (Allocated=0)
		expected          map[string]int32
	}{
		{
			name:              "Scale Up - Waterfill (Fill C1 then Spill to C2)",
			scores:            map[string]int64{"c1": 100, "c2": 90},
			existingPlacement: map[string]int32{"c1": 2, "c2": 0},
			totalReplicas:     4,
			clusters: map[string]string{
				"c1": "3",   // Cap 3
				"c2": "100", // Cap 100
			},
			expected: map[string]int32{"c1": 3, "c2": 1},
		},
		{
			name:              "Scale Down - Tail Removal",
			scores:            map[string]int64{"c1": 100, "c2": 90},
			existingPlacement: map[string]int32{"c1": 3, "c2": 2},
			totalReplicas:     4, // Remove 1 from total 5
			clusters:          map[string]string{"c1": "10", "c2": "10"},
			expected:          map[string]int32{"c1": 3, "c2": 1},
		},
		{
			name:              "Sequential Scale Up - Start from populated tail",
			scores:            map[string]int64{"c1": 100, "c2": 90, "c3": 80},
			existingPlacement: map[string]int32{"c1": 3, "c2": 1, "c3": 0},
			totalReplicas:     6, // Need +2 (Total 4 -> 6)
			clusters:          map[string]string{"c1": "3", "c2": "5", "c3": "5"},
			// c1 is full (3/3). C2 has 1/5. Tail is C2. Add 2 to C2. Result C2=3.
			expected: map[string]int32{"c1": 3, "c2": 3, "c3": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := &schedcache.Snapshot{
				Clusters: make(map[string]*schedcache.ClusterInfo),
			}
			for name, cap := range tt.clusters {
				snapshot.Clusters[name] = &schedcache.ClusterInfo{
					Cluster: createCluster(name, cap),
				}
			}

			appCopy := app.DeepCopy()
			replicas := tt.totalReplicas
			appCopy.Spec.Replicas = &replicas

			var topology []appsv1alpha1.ClusterTopology
			for name, count := range tt.existingPlacement {
				if count > 0 {
					topology = append(topology, appsv1alpha1.ClusterTopology{Name: name, Replicas: count})
				}
			}
			appCopy.Status.Placement.Topology = topology

			result := s.selectClustersStatefulSetWaterfill(appCopy, tt.scores, snapshot)

			resultMap := make(map[string]int32)
			for _, t := range result {
				resultMap[t.Name] = t.Replicas
			}

			// Handle 0s and sort consistency checking if needed.
			// Filter expectations
			for k, v := range tt.expected {
				if v == 0 {
					delete(tt.expected, k)
				}
			}

			// Helper to compare maps equals
			if !assert.Equal(t, len(tt.expected), len(resultMap), "Different map lengths") {
				return
			}
			for k, v := range tt.expected {
				assert.Equal(t, v, resultMap[k], "Different value for key %s", k)
			}
		})
	}
}
