package affinity

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

func TestAffinityFilter(t *testing.T) {
	tests := []struct {
		name           string
		app            *appsv1alpha1.Application
		cluster        *clusterv1alpha1.ManagedCluster
		expectedStatus int
	}{
		{
			name: "no affinity - should pass",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "matching In operator",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"us-west", "us-east"},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "non-matching In operator",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"us-east"},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "matching Exists operator",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpExists,
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "non-matching DoesNotExist operator",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpDoesNotExist,
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
	}

	plugin := New().(*Affinity)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			status := plugin.Filter(context.Background(), state, tt.app, tt.cluster)
			if status.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, status.Code, status.Message)
			}
		})
	}
}

func TestAffinityScore(t *testing.T) {
	tests := []struct {
		name          string
		app           *appsv1alpha1.Application
		cluster       *clusterv1alpha1.ManagedCluster
		expectedScore int64
	}{
		{
			name: "no preferred affinity",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedScore: 0,
		},
		{
			name: "matching preferred affinity",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
							{
								Weight: 50,
								Preference: v1.NodeSelectorTerm{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"us-west"},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedScore: 50,
		},
		{
			name: "non-matching preferred affinity",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
							{
								Weight: 50,
								Preference: v1.NodeSelectorTerm{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"us-east"},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
					},
				},
			},
			expectedScore: 0,
		},
		{
			name: "multiple matching preferred affinities",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterAffinity: &v1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
							{
								Weight: 30,
								Preference: v1.NodeSelectorTerm{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "region",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"us-west"},
										},
									},
								},
							},
							{
								Weight: 40,
								Preference: v1.NodeSelectorTerm{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "env",
											Operator: v1.NodeSelectorOpIn,
											Values:   []string{"prod"},
										},
									},
								},
							},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
					Labels: map[string]string{
						"region": "us-west",
						"env":    "prod",
					},
				},
			},
			expectedScore: 70,
		},
	}

	plugin := New().(*Affinity)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			score, status := plugin.Score(context.Background(), state, tt.app, tt.cluster)
			if !status.IsSuccess() {
				t.Errorf("expected success, got: %s", status.Message)
			}
			if score != tt.expectedScore {
				t.Errorf("expected score %d, got %d", tt.expectedScore, score)
			}
		})
	}
}

func TestAffinityName(t *testing.T) {
	plugin := New()
	if plugin.Name() != Name {
		t.Errorf("expected name %s, got %s", Name, plugin.Name())
	}
}

func TestMatchExpression_NotIn_KeyNotExists(t *testing.T) {
	labels := map[string]string{
		"other": "value",
	}
	expr := v1.NodeSelectorRequirement{
		Key:      "region",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   []string{"us-west"},
	}
	// Key doesn't exist - NotIn should return true
	if !matchExpression(expr, labels) {
		t.Error("NotIn should return true when key doesn't exist")
	}
}

func TestMatchExpression_NotIn_KeyExistsValueNotInList(t *testing.T) {
	labels := map[string]string{
		"region": "us-east",
	}
	expr := v1.NodeSelectorRequirement{
		Key:      "region",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   []string{"us-west", "eu-west"},
	}
	if !matchExpression(expr, labels) {
		t.Error("NotIn should return true when value is not in list")
	}
}

func TestMatchExpression_NotIn_KeyExistsValueInList(t *testing.T) {
	labels := map[string]string{
		"region": "us-west",
	}
	expr := v1.NodeSelectorRequirement{
		Key:      "region",
		Operator: v1.NodeSelectorOpNotIn,
		Values:   []string{"us-west", "eu-west"},
	}
	if matchExpression(expr, labels) {
		t.Error("NotIn should return false when value is in list")
	}
}

func TestMatchExpression_In_KeyNotExists(t *testing.T) {
	labels := map[string]string{
		"other": "value",
	}
	expr := v1.NodeSelectorRequirement{
		Key:      "region",
		Operator: v1.NodeSelectorOpIn,
		Values:   []string{"us-west"},
	}
	if matchExpression(expr, labels) {
		t.Error("In should return false when key doesn't exist")
	}
}

func TestMatchExpression_Gt(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expr     v1.NodeSelectorRequirement
		expected bool
	}{
		{
			name:   "Gt - key not exists",
			labels: map[string]string{},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpGt,
				Values:   []string{"1.0"},
			},
			expected: false,
		},
		{
			name:   "Gt - no values",
			labels: map[string]string{"version": "2.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpGt,
				Values:   []string{},
			},
			expected: false,
		},
		{
			name:   "Gt - value greater",
			labels: map[string]string{"version": "2.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpGt,
				Values:   []string{"1.0"},
			},
			expected: true,
		},
		{
			name:   "Gt - value less",
			labels: map[string]string{"version": "1.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpGt,
				Values:   []string{"2.0"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchExpression(tt.expr, tt.labels)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMatchExpression_Lt(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expr     v1.NodeSelectorRequirement
		expected bool
	}{
		{
			name:   "Lt - key not exists",
			labels: map[string]string{},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpLt,
				Values:   []string{"2.0"},
			},
			expected: false,
		},
		{
			name:   "Lt - no values",
			labels: map[string]string{"version": "1.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpLt,
				Values:   []string{},
			},
			expected: false,
		},
		{
			name:   "Lt - value less",
			labels: map[string]string{"version": "1.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpLt,
				Values:   []string{"2.0"},
			},
			expected: true,
		},
		{
			name:   "Lt - value greater",
			labels: map[string]string{"version": "3.0"},
			expr: v1.NodeSelectorRequirement{
				Key:      "version",
				Operator: v1.NodeSelectorOpLt,
				Values:   []string{"2.0"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchExpression(tt.expr, tt.labels)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMatchExpression_UnknownOperator(t *testing.T) {
	labels := map[string]string{"key": "value"}
	expr := v1.NodeSelectorRequirement{
		Key:      "key",
		Operator: "UnknownOp",
		Values:   []string{"value"},
	}
	if matchExpression(expr, labels) {
		t.Error("Unknown operator should return false")
	}
}

func TestMatchNodeSelectorTerm_EmptyTerm(t *testing.T) {
	labels := map[string]string{"key": "value"}
	term := v1.NodeSelectorTerm{
		MatchExpressions: []v1.NodeSelectorRequirement{},
		MatchFields:      []v1.NodeSelectorRequirement{},
	}
	// Empty term should match anything
	if !matchNodeSelectorTerm(term, labels) {
		t.Error("Empty term should match any labels")
	}
}

func TestMatchNodeSelectorTerm_WithMatchFields(t *testing.T) {
	labels := map[string]string{"field": "value"}
	term := v1.NodeSelectorTerm{
		MatchFields: []v1.NodeSelectorRequirement{
			{
				Key:      "field",
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{"value"},
			},
		},
	}
	if !matchNodeSelectorTerm(term, labels) {
		t.Error("MatchFields should work like MatchExpressions")
	}
}

func TestAffinityScoreExtensions(t *testing.T) {
	plugin := New().(*Affinity)
	ext := plugin.ScoreExtensions()
	if ext != plugin {
		t.Error("ScoreExtensions should return the plugin itself")
	}
}

func TestAffinityNormalizeScore(t *testing.T) {
	plugin := New().(*Affinity)
	state := framework.NewCycleState()
	app := &appsv1alpha1.Application{}

	tests := []struct {
		name     string
		scores   map[string]int64
		expected map[string]int64
	}{
		{
			name:     "all zeros",
			scores:   map[string]int64{"c1": 0, "c2": 0},
			expected: map[string]int64{"c1": 0, "c2": 0},
		},
		{
			name:     "normalize to 100",
			scores:   map[string]int64{"c1": 50, "c2": 100},
			expected: map[string]int64{"c1": 50, "c2": 100},
		},
		{
			name:     "scale up",
			scores:   map[string]int64{"c1": 25, "c2": 50},
			expected: map[string]int64{"c1": 50, "c2": 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := make(map[string]int64)
			for k, v := range tt.scores {
				scores[k] = v
			}
			status := plugin.NormalizeScore(context.Background(), state, app, scores)
			if !status.IsSuccess() {
				t.Errorf("expected success, got: %s", status.Message)
			}
			for k, v := range tt.expected {
				if scores[k] != v {
					t.Errorf("cluster %s: expected %d, got %d", k, v, scores[k])
				}
			}
		})
	}
}

func TestAffinityFilter_MultipleTerms(t *testing.T) {
	plugin := New().(*Affinity)
	state := framework.NewCycleState()

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			ClusterAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							// First term doesn't match
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "region",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"us-east"},
								},
							},
						},
						{
							// Second term matches
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "env",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
					},
				},
			},
		},
	}

	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"region": "us-west",
				"env":    "prod",
			},
		},
	}

	status := plugin.Filter(context.Background(), state, app, cluster)
	// OR logic - if any term matches, it should pass
	if status.Code != framework.Success {
		t.Errorf("expected Success (OR logic), got %d: %s", status.Code, status.Message)
	}
}

func TestAffinityFilter_MultipleExpressions(t *testing.T) {
	plugin := New().(*Affinity)
	state := framework.NewCycleState()

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			ClusterAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							// Both expressions must match (AND logic)
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "region",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"us-west"},
								},
								{
									Key:      "env",
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Missing env label - should fail
	cluster1 := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"region": "us-west",
			},
		},
	}

	status1 := plugin.Filter(context.Background(), state, app, cluster1)
	if status1.Code != framework.Unschedulable {
		t.Errorf("expected Unschedulable (AND logic), got %d", status1.Code)
	}

	// Both labels present - should pass
	cluster2 := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster2",
			Labels: map[string]string{
				"region": "us-west",
				"env":    "prod",
			},
		},
	}

	status2 := plugin.Filter(context.Background(), state, app, cluster2)
	if status2.Code != framework.Success {
		t.Errorf("expected Success, got %d: %s", status2.Code, status2.Message)
	}
}
