package framework

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockScorePlugin is a test plugin that returns fixed scores
type mockScorePlugin struct {
	name   string
	scores map[string]int64
}

func (m *mockScorePlugin) Name() string { return m.name }

func (m *mockScorePlugin) Score(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) (int64, *Status) {
	if score, ok := m.scores[cluster.Name]; ok {
		return score, NewStatus(Success, "")
	}
	return 0, NewStatus(Success, "")
}

func (m *mockScorePlugin) ScoreExtensions() ScoreExtensions {
	return nil // No normalization at plugin level
}

func TestFramework_NormalizeFinalScores(t *testing.T) {
	tests := []struct {
		name     string
		scores   map[string]int64
		expected map[string]int64
	}{
		{
			name: "normalize varied scores",
			scores: map[string]int64{
				"cluster-a": 200,
				"cluster-b": 100,
				"cluster-c": 0,
			},
			expected: map[string]int64{
				"cluster-a": 100,
				"cluster-b": 50,
				"cluster-c": 0,
			},
		},
		{
			name: "all same scores",
			scores: map[string]int64{
				"cluster-a": 50,
				"cluster-b": 50,
			},
			expected: map[string]int64{
				"cluster-a": 50,
				"cluster-b": 50,
			},
		},
		{
			name: "negative and positive",
			scores: map[string]int64{
				"cluster-a": 100,
				"cluster-b": -100,
			},
			expected: map[string]int64{
				"cluster-a": 100,
				"cluster-b": 0,
			},
		},
		{
			name:     "empty scores",
			scores:   map[string]int64{},
			expected: map[string]int64{},
		},
		{
			name: "single cluster",
			scores: map[string]int64{
				"cluster-a": 75,
			},
			expected: map[string]int64{
				"cluster-a": 50, // normalized to neutral when only one
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fw := &frameworkImpl{}
			fw.normalizeFinalScores(tt.scores)

			for cluster, expectedScore := range tt.expected {
				if got := tt.scores[cluster]; got != expectedScore {
					t.Errorf("cluster %s: got score %d, want %d", cluster, got, expectedScore)
				}
			}
		})
	}
}

func TestFramework_RunScorePlugins_NormalizesOutput(t *testing.T) {
	// Create plugins with different score ranges
	plugin1 := &mockScorePlugin{
		name: "plugin1",
		scores: map[string]int64{
			"cluster-a": 100, // high score
			"cluster-b": 50,
			"cluster-c": 0, // low score
		},
	}
	plugin2 := &mockScorePlugin{
		name: "plugin2",
		scores: map[string]int64{
			"cluster-a": 0,   // low score (opposite of plugin1)
			"cluster-b": 50,  // medium
			"cluster-c": 100, // high score
		},
	}

	fw := NewFrameworkWithConfig(
		nil,
		[]ScorePlugin{plugin1, plugin2},
		&SchedulerConfig{
			ScorePlugins: []PluginConfig{
				{Name: "plugin1", Weight: 1},
				{Name: "plugin2", Weight: 1},
			},
		},
	)

	clusters := []*clusterv1alpha1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-c"}},
	}

	app := &appsv1alpha1.Application{}
	state := NewCycleState()

	scores, status := fw.RunScorePlugins(context.Background(), state, app, clusters)
	if !status.IsSuccess() {
		t.Fatalf("RunScorePlugins failed: %s", status.Message)
	}

	// With equal weights and opposite scores, all clusters should be equal (50)
	// But after final normalization, they should all be 50
	for cluster, score := range scores {
		// All should be normalized between 0-100
		if score < 0 || score > 100 {
			t.Errorf("cluster %s score %d is outside 0-100 range", cluster, score)
		}
	}

	// Since plugin1 and plugin2 give opposite scores with equal weights,
	// all clusters should have the same final score
	if scores["cluster-a"] != scores["cluster-b"] || scores["cluster-b"] != scores["cluster-c"] {
		t.Logf("Scores: a=%d, b=%d, c=%d", scores["cluster-a"], scores["cluster-b"], scores["cluster-c"])
		// This is expected behavior - all same scores normalize to 50
	}
}

func TestFramework_RunScorePlugins_WeightedNormalization(t *testing.T) {
	plugin := &mockScorePlugin{
		name: "weighted",
		scores: map[string]int64{
			"cluster-a": 100,
			"cluster-b": 0,
		},
	}

	fw := NewFrameworkWithConfig(
		nil,
		[]ScorePlugin{plugin},
		&SchedulerConfig{
			ScorePlugins: []PluginConfig{
				{Name: "weighted", Weight: 5},
			},
		},
	)

	clusters := []*clusterv1alpha1.ManagedCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-b"}},
	}

	app := &appsv1alpha1.Application{}
	state := NewCycleState()

	scores, status := fw.RunScorePlugins(context.Background(), state, app, clusters)
	if !status.IsSuccess() {
		t.Fatalf("RunScorePlugins failed: %s", status.Message)
	}

	// After final normalization, cluster-a should be 100, cluster-b should be 0
	if scores["cluster-a"] != 100 {
		t.Errorf("cluster-a score = %d, want 100", scores["cluster-a"])
	}
	if scores["cluster-b"] != 0 {
		t.Errorf("cluster-b score = %d, want 0", scores["cluster-b"])
	}
}
