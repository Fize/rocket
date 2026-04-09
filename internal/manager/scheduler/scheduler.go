package scheduler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/hex-techs/rocket/internal/manager/scheduler/cache"
	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	"github.com/hex-techs/rocket/internal/manager/scheduler/metrics"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/affinity"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/capacity"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/health"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/resource"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/taint"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/topology"
	"github.com/hex-techs/rocket/internal/manager/scheduler/plugins/volumerestriction"
	"github.com/hex-techs/rocket/internal/manager/scheduler/queue"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/hex-techs/rocket/pkg/observability"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DefaultTopologyKey is the default label key used for topology spread
const DefaultTopologyKey = "topology.kubernetes.io/zone"

type Scheduler struct {
	client client.Client
	cache  cache.Cache
	queue  queue.SchedulingQueue

	framework framework.Framework
	config    *framework.SchedulerConfig
}

func NewScheduler(client client.Client, cache cache.Cache, queue queue.SchedulingQueue) *Scheduler {
	return NewSchedulerWithConfig(client, cache, queue, nil)
}

func NewSchedulerWithConfig(client client.Client, cache cache.Cache, queue queue.SchedulingQueue, config *framework.SchedulerConfig) *Scheduler {
	if config == nil {
		config = framework.DefaultSchedulerConfig()
	}

	// Build filter plugins based on config
	filterPlugins := make([]framework.FilterPlugin, 0)
	for _, pc := range config.FilterPlugins {
		if !pc.Enabled {
			continue
		}
		var pl framework.Plugin
		switch pc.Name {
		case "Health":
			pl = health.New()
		case "Affinity":
			pl = affinity.New()
		case "TaintToleration":
			pl = taint.New()
		case "Capacity":
			pl = capacity.New()
		case "VolumeRestriction":
			pl = volumerestriction.New()
		}
		if pl != nil {
			if fp, ok := pl.(framework.FilterPlugin); ok {
				filterPlugins = append(filterPlugins, fp)
			}
		}
	}

	// Build score plugins based on config
	scorePlugins := make([]framework.ScorePlugin, 0)
	for _, pc := range config.ScorePlugins {
		if !pc.Enabled {
			continue
		}
		var pl framework.Plugin
		switch pc.Name {
		case "Affinity":
			pl = affinity.New()
		case "Resource":
			// Check if strategy is specified in args
			if strategy, ok := pc.Args["strategy"].(string); ok {
				pl = resource.NewWithStrategy(strategy)
			} else {
				pl = resource.New()
			}
		case "TopologySpread":
			// Check if topology key is specified in args
			if key, ok := pc.Args["topologyKey"].(string); ok {
				pl = topology.NewWithTopologyKey(key)
			} else {
				pl = topology.New()
			}
		}
		if pl != nil {
			if sp, ok := pl.(framework.ScorePlugin); ok {
				scorePlugins = append(scorePlugins, sp)
			}
		}
	}

	fw := framework.NewFrameworkWithConfig(filterPlugins, scorePlugins, config)

	return &Scheduler{
		client:    client,
		cache:     cache,
		queue:     queue,
		framework: fw,
		config:    config,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	logger := log.FromContext(ctx)
	logger.Info("Starting scheduler")

	wait.UntilWithContext(ctx, s.scheduleOne, time.Second)

	logger.Info("Stopping scheduler")
}

func (s *Scheduler) scheduleOne(ctx context.Context) {
	logger := observability.TraceLogger(ctx, log.FromContext(ctx))
	startTime := time.Now()
	var scheduleResult string = "error"

	// Update queue length metric
	metrics.SetQueueLength(s.queue.Len())

	app, err := s.queue.Pop()
	if err != nil {
		logger.Error(err, "Failed to pop application from queue")
		return
	}

	ctx, span := observability.Tracer().Start(ctx, "Scheduler.ScheduleOne",
		trace.WithAttributes(
			attribute.String("application.name", app.Name),
		),
	)
	defer span.End()

	// Ensure we call Done when finished processing (standard workqueue pattern)
	defer func() {
		s.queue.Done(app)
		metrics.RecordSchedulingAttempt(scheduleResult, time.Since(startTime))
	}()

	logger.Info("Attempting to schedule application", "application", app.Name)

	// 1. Snapshot
	snapshot := s.cache.Snapshot()
	if len(snapshot.Clusters) == 0 {
		logger.Info("No clusters available", "application", app.Name)
		scheduleResult = "unschedulable"
		s.requeue(app, fmt.Errorf("no clusters available"))
		return
	}

	// 2. Pre-filter using Indexer for affinity (optimization)
	candidateClusters := s.prefilterByAffinity(app, snapshot)

	// 2.1 Filter
	state := framework.NewCycleState()
	feasibleClusters := make([]*clusterv1alpha1.ManagedCluster, 0)

	for _, clusterInfo := range candidateClusters {
		cluster := clusterInfo.Cluster
		status := s.framework.RunFilterPlugins(ctx, state, app, cluster)
		if status.IsSuccess() {
			feasibleClusters = append(feasibleClusters, cluster)
		} else {
			logger.V(1).Info("Cluster filtered out", "cluster", cluster.Name, "reason", status.Message)
		}
	}

	// Record filtering metrics
	metrics.RecordFilterResults(len(candidateClusters), len(feasibleClusters))

	if len(feasibleClusters) == 0 {
		logger.Info("No feasible clusters found", "application", app.Name)
		scheduleResult = "unschedulable"
		s.requeue(app, fmt.Errorf("no feasible clusters found"))
		return
	}

	// 2.5. Initialize topology distribution for TopologySpread scoring
	// Gather existing placements from all scheduled applications
	// NOTE: We use ALL clusters from snapshot (not just feasibleClusters) to correctly
	// calculate topology distribution across the entire cluster fleet
	existingPlacements := s.gatherExistingPlacements(ctx, snapshot)
	topologyKey := s.getTopologyKey()
	allClusters := s.getAllClustersFromSnapshot(snapshot)
	topology.UpdateTopologyDistribution(state, allClusters, existingPlacements, topologyKey)

	// 3. Score
	scores, status := s.framework.RunScorePlugins(ctx, state, app, feasibleClusters)
	if !status.IsSuccess() {
		logger.Error(nil, "Failed to score clusters", "reason", status.Message)
		s.requeue(app, fmt.Errorf("scoring failed: %s", status.Message))
		return
	}

	// 4. Select
	var placement []appsv1alpha1.ClusterTopology

	// Determine strategy: Annotation > Global Config
	strategy := s.config.Strategy
	if val, ok := app.Annotations[framework.AnnotationSchedulerStrategy]; ok {
		if val == framework.StrategySpread || val == framework.StrategySingleCluster {
			strategy = val
		}
	}

	if strategy == framework.StrategySpread {
		if app.Spec.Workload.Kind == "StatefulSet" {
			placement = s.selectClustersStatefulSetWaterfill(app, scores, snapshot)
		} else {
			placement = s.selectClustersSpread(app, scores, snapshot)
		}
	} else {
		// Default: SingleCluster strategy
		selectedCluster := s.selectCluster(scores)
		replicas := int32(1)
		if app.Spec.Replicas != nil {
			replicas = *app.Spec.Replicas
		}
		placement = []appsv1alpha1.ClusterTopology{
			{
				Name:     selectedCluster,
				Replicas: replicas,
			},
		}
	}

	if len(placement) == 0 {
		logger.Info("No clusters selected", "application", app.Name)
		scheduleResult = "unschedulable"
		s.requeue(app, fmt.Errorf("no clusters selected"))
		return
	}

	// 5. Bind
	if err := s.bind(ctx, app, placement); err != nil {
		logger.Error(err, "Failed to bind application", "application", app.Name)
		s.requeue(app, err)
		return
	}

	// Optimistically assume the application in the cache to prevent over-scheduling
	// before the cluster status is updated.
	for _, p := range placement {
		if err := s.cache.AssumeApplication(app, p.Name, p.Replicas); err != nil {
			logger.Error(err, "Failed to assume application in cache", "cluster", p.Name)
		}
	}

	// Success! Clear rate limiter history for this item
	s.queue.Forget(app)
	scheduleResult = "success"
	logger.Info("Successfully scheduled application", "application", app.Name, "placement", placement)
}

func (s *Scheduler) selectCluster(scores map[string]int64) string {
	var selected string
	var maxScore int64 = -1

	// Sort keys for deterministic behavior
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, cluster := range keys {
		score := scores[cluster]
		if score > maxScore {
			maxScore = score
			selected = cluster
		}
	}
	return selected
}

// selectClustersSpread distributes replicas across multiple clusters based on scores
// Scaled-Up Logic: Ensure we never reduce replicas on existing clusters during scale-up.
func (s *Scheduler) selectClustersSpread(app *appsv1alpha1.Application, scores map[string]int64, snapshot *cache.Snapshot) []appsv1alpha1.ClusterTopology {
	totalReplicas := int32(1)
	if app.Spec.Replicas != nil {
		totalReplicas = *app.Spec.Replicas
	}

	// 1. Gather current placement state
	currentPlacement := make(map[string]int32)
	var currentTotal int32 = 0
	if app.Status.Placement.Topology != nil {
		for _, t := range app.Status.Placement.Topology {
			currentPlacement[t.Name] = t.Replicas
			currentTotal += t.Replicas
		}
	}

	isScaleUp := totalReplicas > currentTotal

	// Sort clusters by score (descending)
	type clusterScore struct {
		name  string
		score int64
	}
	clusterScores := make([]clusterScore, 0, len(scores))
	for name, score := range scores {
		clusterScores = append(clusterScores, clusterScore{name: name, score: score})
	}
	sort.Slice(clusterScores, func(i, j int) bool {
		if clusterScores[i].score == clusterScores[j].score {
			return clusterScores[i].name < clusterScores[j].name // tie-breaker
		}
		return clusterScores[i].score > clusterScores[j].score
	})

	// Determine how many clusters to use
	maxClusters := s.config.SpreadConstraints.MaxClusters
	if maxClusters <= 0 {
		maxClusters = 5 // default
	}
	if maxClusters > len(clusterScores) {
		maxClusters = len(clusterScores)
	}

	minReplicas := s.config.SpreadConstraints.MinReplicas
	if minReplicas <= 0 {
		minReplicas = 1 // default
	}

	// Calculate how many clusters we can actually use given minReplicas
	// Note: During scale-up, we might already be using some clusters.
	// The distribution logic needs to be careful not to violate the "no reduction" rule.

	// Calculate base max usable clusters from pure math
	maxUsableClusters := int(totalReplicas / minReplicas)
	if maxUsableClusters < 1 {
		maxUsableClusters = 1
	}

	// If the user already has more clusters than maxUsable, we shouldn't force reduce them strictly?
	// But usually scale up means totalReplicas increased, so maxUsableClusters likely increased or stayed same.

	if maxUsableClusters < maxClusters {
		maxClusters = maxUsableClusters
	}

	// Strategy:
	// We want to calculate an "Ideal Distribution" (D_ideal) based on scores,
	// BUT constrained by "Current Distribution" (D_current).
	// For every cluster C_i:
	//   If ScaleUp:  D_new[i] >= D_current[i]
	//   If ScaleDown: D_new[i] <= D_current[i] (Strictly speaking, just reducing total is enough, but usually we want to respect scores)

	// However, the user specifically asked: "In ScaleUp, cluster X had 3, calculated ideal is 2 -> It should stay 3 (or grow)".
	// So we must effectively reserve D_current[i] from the TotalReplicas first, and only distribute the Delta.
	// OR, we calculate Ideal, and then apply `max(Ideal, Current)` and work out the difference.

	// Let's implement a hybrid approach:
	// 1. Calculate weights and ideal shares based on Scores usually.
	// 2. Adjust shares to satisfy D_new[i] >= D_current[i].

	// Identify candidate clusters (Top N)
	// We must include ALL currently used clusters to avoid abandoning them (unless maxClusters forces us to, but even then it's risky for ScaleUp)
	// So candidates = Union(Top Scored, Currently Used)

	candidateMap := make(map[string]bool)
	candidateList := make([]clusterScore, 0)

	// Add currently used clusters first (to ensure they are considered)
	for name := range currentPlacement {
		candidateMap[name] = true
		// Find score
		score := scores[name] // might be 0 if not feasible anymore? Assuming feasible from filter stage.
		// If filter stage removed it, it won't be in scores. If it's running but now filtered (e.g. Taint), we strictly speaking should evict it?
		// But for "ScaleUp implies No Reduction", we should prob keep it if possible.
		// For simplicity, assume feasible.
		candidateList = append(candidateList, clusterScore{name: name, score: score})
	}

	// Fill remaining slots with best scored new clusters
	for _, cs := range clusterScores {
		if len(candidateList) >= maxClusters {
			break
		}
		if !candidateMap[cs.name] {
			candidateMap[cs.name] = true
			candidateList = append(candidateList, cs)
		}
	}

	selectedClusters := candidateList

	// Calculate weights based on Score
	weights := make(map[string]float64)
	var totalWeight float64 = 0

	for _, cs := range selectedClusters {
		w := float64(cs.score)
		weights[cs.name] = w
		totalWeight += w
	}

	if totalWeight == 0 {
		// Equal distribution if all weights are 0
		totalWeight = float64(len(selectedClusters))
		for i := range selectedClusters {
			weights[selectedClusters[i].name] = 1
		}
	}

	// Prepare allocations
	type allocation struct {
		name      string
		score     int64
		current   int32
		quota     int32
		remainder float64
		weight    float64
	}

	allocs := make([]allocation, 0, len(selectedClusters))

	// Initial Pass: Calculate ideal weighted share
	// But we must enforce minimums.

	// If ScaleUp: Minimum = Current
	// If ScaleDown: Minimum = 0 (we allowed to reduce)

	for _, cs := range selectedClusters {
		w := weights[cs.name]
		curr := currentPlacement[cs.name]

		allocs = append(allocs, allocation{
			name:    cs.name,
			score:   cs.score,
			current: curr,
			weight:  w,
			quota:   0, // to be calculated
		})
	}

	// Algorithm for "Proportional Distribution with Minimum Floors":
	// 1. Assign each cluster its Minimum (Current).
	// 2. Distribute the `Remaining = Total - Sum(Current)` according to weights.
	// IF Total < Sum(Current), then we are in ScaleDown mode (or specific manual reduction).

	if isScaleUp {
		// --- SCALE UP LOGIC ---
		// Rule: New[i] >= Current[i]

		var assignedTotal int32 = 0

		// 1. Base assignment = Current
		for i := range allocs {
			allocs[i].quota = allocs[i].current
			assignedTotal += allocs[i].quota
		}

		// 2. Distribute the growth delta
		remaining := totalReplicas - assignedTotal

		// If remaining > 0, distribute based on Deficit (Ideal - Current)
		// This ensures we converge towards ideal distribution while preserving existing replicas.
		if remaining > 0 {
			// Calculate deficits
			var totalDeficit float64
			deficits := make([]float64, len(allocs))

			for i := range allocs {
				ideal := float64(totalReplicas) * allocs[i].weight / totalWeight
				deficit := math.Max(0, ideal-float64(allocs[i].current))
				deficits[i] = deficit
				totalDeficit += deficit
			}

			// Sub-distribute 'remaining' based on deficits
			// If totalDeficit is 0 (all saturated), fall back to weights
			useWeights := totalDeficit == 0

			for i := range allocs {
				var share float64
				if useWeights {
					share = float64(remaining) * allocs[i].weight / totalWeight
				} else {
					share = float64(remaining) * deficits[i] / totalDeficit
				}

				q := int32(math.Floor(share))
				allocs[i].quota += q
				allocs[i].remainder = share - float64(q)
				assignedTotal += q
			}

			// Distribute remainders (Largest Remainder Method)
			remaining = totalReplicas - assignedTotal

			// Sort by remainder desc
			sort.Slice(allocs, func(i, j int) bool {
				return allocs[i].remainder > allocs[j].remainder
			})

			for i := 0; i < int(remaining); i++ {
				allocs[i].quota++
			}
		}

	} else {
		// --- SCALE DOWN LOGIC ---
		// Rule: Prefer reducing from clusters that are OVER their ideal quota.
		// This minimizes unnecessary Pod migrations.

		// Calculate ideal distribution first
		idealAllocs := make([]float64, len(allocs))
		for i := range allocs {
			idealAllocs[i] = float64(totalReplicas) * allocs[i].weight / totalWeight
		}

		// Calculate how much to reduce
		toReduce := currentTotal - totalReplicas

		// Calculate surplus (how much each cluster is over ideal)
		surpluses := make([]float64, len(allocs))
		var totalSurplus float64
		for i := range allocs {
			surplus := math.Max(0, float64(allocs[i].current)-idealAllocs[i])
			surpluses[i] = surplus
			totalSurplus += surplus
		}

		// Start with current allocation
		for i := range allocs {
			allocs[i].quota = allocs[i].current
		}

		// Distribute reduction based on surplus
		if totalSurplus > 0 {
			// Reduce from surplus clusters proportionally
			for i := range allocs {
				reduction := float64(toReduce) * surpluses[i] / totalSurplus
				r := int32(math.Floor(reduction))
				allocs[i].quota -= r
				allocs[i].remainder = reduction - float64(r)
			}

			// Calculate how much we still need to reduce
			var currentAssigned int32
			for _, a := range allocs {
				currentAssigned += a.quota
			}
			stillToReduce := currentAssigned - totalReplicas

			// Sort by remainder desc (higher remainder = should reduce more)
			sort.Slice(allocs, func(i, j int) bool {
				return allocs[i].remainder > allocs[j].remainder
			})

			for i := 0; i < int(stillToReduce); i++ {
				allocs[i].quota--
			}
		} else {
			// No surplus - all clusters are at or below ideal
			// Fall back to reducing proportionally by weight (inverse)
			// Reduce more from lower-weight clusters
			var inverseWeightTotal float64
			for i := range allocs {
				if allocs[i].weight > 0 {
					inverseWeightTotal += 1.0 / allocs[i].weight
				}
			}

			if inverseWeightTotal > 0 {
				for i := range allocs {
					if allocs[i].weight > 0 {
						reduction := float64(toReduce) * (1.0 / allocs[i].weight) / inverseWeightTotal
						r := int32(math.Floor(reduction))
						allocs[i].quota -= r
						allocs[i].remainder = reduction - float64(r)
					}
				}
			}

			var currentAssigned int32
			for _, a := range allocs {
				currentAssigned += a.quota
			}
			stillToReduce := currentAssigned - totalReplicas

			sort.Slice(allocs, func(i, j int) bool {
				return allocs[i].remainder > allocs[j].remainder
			})

			for i := 0; i < int(stillToReduce); i++ {
				allocs[i].quota--
			}
		}

		// Ensure no negative quotas
		for i := range allocs {
			if allocs[i].quota < 0 {
				allocs[i].quota = 0
			}
		}
	}

	// Build result
	result := make([]appsv1alpha1.ClusterTopology, 0, len(allocs))
	for _, a := range allocs {
		if a.quota > 0 {
			result = append(result, appsv1alpha1.ClusterTopology{
				Name:     a.name,
				Replicas: a.quota,
			})
		}
	}

	// Sort by Name for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func (s *Scheduler) selectClustersStatefulSetWaterfill(app *appsv1alpha1.Application, scores map[string]int64, snapshot *cache.Snapshot) []appsv1alpha1.ClusterTopology {
	totalReplicas := int32(1)
	if app.Spec.Replicas != nil {
		totalReplicas = *app.Spec.Replicas
	}

	// 1. Gather current placement
	existingPlacement := make(map[string]int32)
	var currentTotal int32 = 0
	if app.Status.Placement.Topology != nil {
		for _, t := range app.Status.Placement.Topology {
			existingPlacement[t.Name] = t.Replicas
			currentTotal += t.Replicas
		}
	}

	// 2. Identify Candidates (Consistent Order)
	clusterNames := make(map[string]struct{})
	for name := range scores {
		clusterNames[name] = struct{}{}
	}
	for name := range existingPlacement {
		clusterNames[name] = struct{}{}
	}

	sortedClusters := make([]string, 0, len(clusterNames))
	for name := range clusterNames {
		sortedClusters = append(sortedClusters, name)
	}
	sort.Strings(sortedClusters)

	newPlacement := make(map[string]int32)
	for name, replicas := range existingPlacement {
		newPlacement[name] = replicas
	}

	if totalReplicas > currentTotal {
		// Scale Up: Fill sequentially starting from the first "unsatisfied" or "tail" cluster
		needed := totalReplicas - currentTotal

		// Find the starting point:
		// We should start filling from the last cluster that currently has replicas.
		// If no clusters have replicas, start from index 0.
		startIndex := 0
		for i, name := range sortedClusters {
			if existingPlacement[name] > 0 {
				startIndex = i
			}
		}

		for i := startIndex; i < len(sortedClusters); i++ {
			if needed == 0 {
				break
			}
			name := sortedClusters[i]
			current := newPlacement[name]

			// Calculate Capacity
			var cap int32 = math.MaxInt32
			if info, ok := snapshot.Clusters[name]; ok {
				calc := capacity.CalculateMaxReplicas(info.Cluster, app)
				if calc >= 0 && calc < int64(math.MaxInt32) {
					cap = int32(calc)
				}
			} else {
				// Cluster unavailable
				if _, ok := scores[name]; !ok {
					// If not feasible, freeze it (cap = current)
					cap = current
				}
			}

			room := cap - current
			if room < 0 {
				room = 0
			}

			take := needed
			if take > room {
				take = room
			}

			newPlacement[name] += take
			needed -= take
		}

		// If still needed (overflow), dump on the last available cluster
		if needed > 0 && len(sortedClusters) > 0 {
			// Use the last cluster in the list
			last := sortedClusters[len(sortedClusters)-1]
			newPlacement[last] += needed
		}

	} else if totalReplicas < currentTotal {
		// Scale Down: Remove from last active cluster first
		remove := currentTotal - totalReplicas

		for i := len(sortedClusters) - 1; i >= 0; i-- {
			if remove == 0 {
				break
			}
			name := sortedClusters[i]
			current := newPlacement[name]
			if current == 0 {
				continue
			}

			drop := remove
			if drop > current {
				drop = current
			}

			newPlacement[name] -= drop
			remove -= drop
		}
	}

	// Build result
	result := make([]appsv1alpha1.ClusterTopology, 0, len(newPlacement))
	for _, name := range sortedClusters {
		count := newPlacement[name]
		if count > 0 {
			result = append(result, appsv1alpha1.ClusterTopology{
				Name:     name,
				Replicas: count,
			})
		}
	}

	return result
}

func (s *Scheduler) bind(ctx context.Context, app *appsv1alpha1.Application, placement []appsv1alpha1.ClusterTopology) error {
	// Fetch latest version
	latestApp := &appsv1alpha1.Application{}
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(app), latestApp); err != nil {
		return err
	}

	latestApp.Status.Placement = appsv1alpha1.PlacementStatus{
		Topology: placement,
	}
	latestApp.Status.SchedulingPhase = appsv1alpha1.Scheduled

	return s.client.Status().Update(ctx, latestApp)
}

func (s *Scheduler) requeue(app *appsv1alpha1.Application, reason error) {
	logger := log.Log.WithName("scheduler")

	// Record retry metric
	metrics.RecordRetry()

	// Create QueuedApplicationInfo to track retry attempts
	qInfo := &queue.QueuedApplicationInfo{
		Application: app,
		Attempts:    1, // Increment will be handled by queue implementation
	}

	// Use Requeue for rate-limited retry (exponential backoff)
	s.queue.Requeue(app)

	reasonMsg := "unknown"
	if reason != nil {
		reasonMsg = reason.Error()
	}
	logger.V(1).Info("Application requeued for retry", "application", app.Name, "namespace", app.Namespace, "reason", reasonMsg, "attempts", qInfo.Attempts)
}

// getAllClustersFromSnapshot returns all clusters from the snapshot as a slice.
// This is used for topology distribution calculation which needs all clusters,
// not just the feasible ones.
func (s *Scheduler) getAllClustersFromSnapshot(snapshot *cache.Snapshot) []*clusterv1alpha1.ManagedCluster {
	clusters := make([]*clusterv1alpha1.ManagedCluster, 0, len(snapshot.Clusters))
	for _, info := range snapshot.Clusters {
		clusters = append(clusters, info.Cluster)
	}
	return clusters
}

// gatherExistingPlacements collects topology placements from all scheduled applications.
// This is used to calculate topology distribution for spread scoring.
// It includes both committed placements from the API server and assumed placements from the cache.
func (s *Scheduler) gatherExistingPlacements(ctx context.Context, snapshot *cache.Snapshot) []appsv1alpha1.ClusterTopology {
	var placements []appsv1alpha1.ClusterTopology

	// 1. Collect committed placements from the API server
	appList := &appsv1alpha1.ApplicationList{}
	if err := s.client.List(ctx, appList); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list applications for topology distribution")
	} else {
		for _, app := range appList.Items {
			if app.Status.SchedulingPhase == appsv1alpha1.Scheduled && len(app.Status.Placement.Topology) > 0 {
				placements = append(placements, app.Status.Placement.Topology...)
			}
		}
	}

	// 2. Collect assumed placements from the cache snapshot
	// We need to be careful not to double-count applications that are both in the API server (as Scheduled)
	// and in the cache (as Assumed).
	assumedApps := make(map[string]map[string]int32) // appKey -> clusterName -> replicas
	for clusterName, info := range snapshot.Clusters {
		for appKey, assumed := range info.AssumedApplications {
			if _, ok := assumedApps[appKey]; !ok {
				assumedApps[appKey] = make(map[string]int32)
			}
			assumedApps[appKey][clusterName] = assumed.Replicas
		}
	}

	// Add assumed placements if they are not already in the committed list
	committedAppKeys := make(map[string]struct{})
	for _, app := range appList.Items {
		if app.Status.SchedulingPhase == appsv1alpha1.Scheduled {
			committedAppKeys[fmt.Sprintf("%s/%s", app.Namespace, app.Name)] = struct{}{}
		}
	}

	for appKey, clusterReplicas := range assumedApps {
		if _, committed := committedAppKeys[appKey]; committed {
			// Skip assumed applications that are already committed in the API server
			continue
		}
		for clusterName, replicas := range clusterReplicas {
			placements = append(placements, appsv1alpha1.ClusterTopology{
				Name:     clusterName,
				Replicas: replicas,
			})
		}
	}

	return placements
}

// getTopologyKey returns the topology key to use for spread scoring.
// It checks the TopologySpread plugin configuration first, then falls back to default.
func (s *Scheduler) getTopologyKey() string {
	if s.config != nil {
		for _, pc := range s.config.ScorePlugins {
			if pc.Name == "TopologySpread" && pc.Args != nil {
				if key, ok := pc.Args["topologyKey"].(string); ok && key != "" {
					return key
				}
			}
		}
	}
	return DefaultTopologyKey
}

// prefilterByAffinity uses the Indexer to pre-filter clusters based on affinity requirements.
// This is an optimization to reduce the number of clusters that need to go through full filter plugins.
// If affinity is not specified or cannot be optimized, it returns all clusters.
func (s *Scheduler) prefilterByAffinity(app *appsv1alpha1.Application, snapshot *cache.Snapshot) []*cache.ClusterInfo {
	affinity := app.Spec.ClusterAffinity
	if affinity == nil || affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		// No affinity requirement, return all clusters
		result := make([]*cache.ClusterInfo, 0, len(snapshot.Clusters))
		for _, info := range snapshot.Clusters {
			result = append(result, info)
		}
		return result
	}

	// Try to use Indexer for label selectors
	// We handle multiple terms (OR logic) and intersect indexable expressions within each term.
	terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) == 0 {
		return s.getAllClusterInfosFromSnapshot(snapshot)
	}

	var allCandidates map[string]struct{}

	for _, term := range terms {
		if len(term.MatchFields) > 0 {
			// MatchFields not supported by indexer, fall back to full scan
			return s.getAllClusterInfosFromSnapshot(snapshot)
		}

		termCandidates := s.prefilterByTermUsingIndexer(term, snapshot)
		if termCandidates == nil {
			// Cannot optimize this term (e.g. only contains NotIn/Gt/Lt), so we must include all clusters (OR logic)
			return s.getAllClusterInfosFromSnapshot(snapshot)
		}

		if allCandidates == nil {
			allCandidates = make(map[string]struct{})
		}
		for _, name := range termCandidates {
			allCandidates[name] = struct{}{}
		}
	}

	if allCandidates == nil {
		// Should not happen if terms > 0, but safe fallback
		return s.getAllClusterInfosFromSnapshot(snapshot)
	}

	// Convert names to ClusterInfo
	result := make([]*cache.ClusterInfo, 0, len(allCandidates))
	for name := range allCandidates {
		if info, ok := snapshot.Clusters[name]; ok {
			result = append(result, info)
		}
	}
	return result

}

// getAllClusterInfosFromSnapshot returns all cluster infos from the snapshot as a slice.
func (s *Scheduler) getAllClusterInfosFromSnapshot(snapshot *cache.Snapshot) []*cache.ClusterInfo {
	result := make([]*cache.ClusterInfo, 0, len(snapshot.Clusters))
	for _, info := range snapshot.Clusters {
		result = append(result, info)
	}
	return result
}

// prefilterByTermUsingIndexer attempts to use the Indexer for a single NodeSelectorTerm.
// Returns nil if the term cannot be optimized (caller should fall back to full iteration).
func (s *Scheduler) prefilterByTermUsingIndexer(term v1.NodeSelectorTerm, snapshot *cache.Snapshot) []string {
	if len(term.MatchExpressions) == 0 {
		return nil
	}

	var candidateSet map[string]struct{}
	var initialized bool

	for _, expr := range term.MatchExpressions {
		var matches []string

		switch expr.Operator {
		case v1.NodeSelectorOpIn:
			// Get clusters matching any of the values
			matchSet := make(map[string]struct{})
			for _, val := range expr.Values {
				for _, name := range snapshot.Indexer.GetClustersByLabel(expr.Key, val) {
					matchSet[name] = struct{}{}
				}
			}
			matches = make([]string, 0, len(matchSet))
			for name := range matchSet {
				matches = append(matches, name)
			}

		case v1.NodeSelectorOpExists:
			// Get all clusters with this label key (any value)
			// Use the optimized GetClustersByLabelKey method
			if simpleIndexer, ok := snapshot.Indexer.(*cache.SimpleIndexer); ok {
				matches = simpleIndexer.GetClustersByLabelKey(expr.Key)
			} else {
				// Fall back if not SimpleIndexer
				return nil
			}

		default:
			// NotIn, DoesNotExist, Gt, Lt cannot be efficiently indexed
			// Skip this expression (treat as match all for pre-filtering)
			continue
		}

		if !initialized {
			candidateSet = make(map[string]struct{})
			for _, name := range matches {
				candidateSet[name] = struct{}{}
			}
			initialized = true
		} else {
			// Intersect with previous results (AND semantics within a term)
			newSet := make(map[string]struct{})
			for _, name := range matches {
				if _, ok := candidateSet[name]; ok {
					newSet[name] = struct{}{}
				}
			}
			candidateSet = newSet
		}

		if len(candidateSet) == 0 {
			return []string{} // No candidates left
		}
	}

	if !initialized {
		// No indexable expressions found, cannot optimize
		return nil
	}

	result := make([]string, 0, len(candidateSet))
	for name := range candidateSet {
		result = append(result, name)
	}
	return result
}
