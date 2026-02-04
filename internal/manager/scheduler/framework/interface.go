package framework

import (
	"context"
	"sync"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

// CycleState stores data that needs to be passed between plugins during a scheduling cycle.
type CycleState struct {
	mx      sync.RWMutex
	storage map[string]interface{}
}

func NewCycleState() *CycleState {
	return &CycleState{
		storage: make(map[string]interface{}),
	}
}

func (c *CycleState) Read(key string) (interface{}, bool) {
	c.mx.RLock()
	defer c.mx.RUnlock()
	v, ok := c.storage[key]
	return v, ok
}

func (c *CycleState) Write(key string, val interface{}) {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.storage[key] = val
}

func (c *CycleState) Delete(key string) {
	c.mx.Lock()
	defer c.mx.Unlock()
	delete(c.storage, key)
}

// Status is the result of a plugin execution.
type Status struct {
	Code    int
	Message string
}

const (
	Success int = iota
	Error
	Unschedulable
)

func NewStatus(code int, msg string) *Status {
	return &Status{
		Code:    code,
		Message: msg,
	}
}

func (s *Status) IsSuccess() bool {
	return s.Code == Success
}

func (s *Status) IsUnschedulable() bool {
	return s.Code == Unschedulable
}

func (s *Status) IsError() bool {
	return s.Code == Error
}

// Plugin is the parent interface for all the scheduling framework plugins.
type Plugin interface {
	Name() string
}

// FilterPlugin is an interface for Filter plugins. These plugins are used to filter out clusters that cannot run the application.
type FilterPlugin interface {
	Plugin
	// Filter is called for each cluster.
	Filter(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *Status
}

// ScorePlugin is an interface that must be implemented by "Score" plugins to rank clusters that passed the filtering phase.
type ScorePlugin interface {
	Plugin
	// Score is called on each filtered cluster. It must return a score in [0, 100].
	Score(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) (int64, *Status)
	// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if does not.
	ScoreExtensions() ScoreExtensions
}

// ScoreExtensions is an interface for Score plugins to implement if they want to normalize scores.
type ScoreExtensions interface {
	// NormalizeScore is called for all node scores produced by the same plugin's "Score" method.
	// A successful execution is required to return a nil status.
	// Where possible, plugins should not depend on this method.
	NormalizeScore(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, scores map[string]int64) *Status
}

// Framework manages the set of plugins in use by the scheduler.
// It is responsible for calling the plugins in the correct order.
type Framework interface {
	// RunFilterPlugins runs the set of configured Filter plugins for application on the given cluster.
	// If any of these plugins returns an Unschedulable status, the cluster is not suitable for running the application.
	RunFilterPlugins(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *Status

	// RunScorePlugins runs the set of configured Score plugins. It returns a map of cluster names to scores.
	// If any of these plugins returns an error, the scheduling cycle is aborted.
	RunScorePlugins(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, clusters []*clusterv1alpha1.ManagedCluster) (map[string]int64, *Status)
}
