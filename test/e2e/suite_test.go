//go:build e2e

// Package e2e provides end-to-end tests for the Rocket multi-cluster management system.
// These tests require a Kind multi-cluster environment to be set up beforehand.
//
// To run the tests:
//  1. Set up the environment: make e2e-setup
//  2. Run the tests: make e2e-test
//  3. Cleanup: make e2e-cleanup
package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fize/rocket/internal/addon"
	"github.com/fize/rocket/pkg/constants"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	_ "github.com/fize/rocket/internal/addon/kruiserollout"
	_ "github.com/fize/rocket/internal/addon/mcs"
	_ "github.com/fize/rocket/internal/addon/victoriametrics"
	"github.com/fize/rocket/internal/agent/cluster"
	addoncontroller "github.com/fize/rocket/internal/manager/addon"
	"github.com/fize/rocket/internal/manager/apiserver/handler"
	"github.com/fize/rocket/internal/manager/application"
	managercluster "github.com/fize/rocket/internal/manager/cluster"
	"github.com/fize/rocket/internal/manager/scheduler"
	"github.com/fize/rocket/internal/manager/scheduler/cache"
	"github.com/fize/rocket/internal/manager/scheduler/framework"
	"github.com/fize/rocket/internal/manager/scheduler/queue"
	"github.com/fize/rocket/internal/manager/sharding"
	"github.com/fize/rocket/internal/manager/workspace"
	"github.com/rancher/remotedialer"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	// TestNamespace is the namespace used for e2e tests
	TestNamespace = "rocket-system"
	// DefaultTimeout is the default timeout for waiting operations
	DefaultTimeout = 30 * time.Second
	// DefaultInterval is the default interval for polling operations
	DefaultInterval = 1 * time.Second
)

// TestEnvironment holds the shared test environment
type TestEnvironment struct {
	Config        *rest.Config
	Client        client.Client
	Manager       ctrl.Manager
	TunnelServer  *remotedialer.Server
	ClientManager *managercluster.ClientManager
	SchedCache    cache.Cache
	SchedQueue    queue.SchedulingQueue
	Scheduler     *scheduler.Scheduler
	ShardManager  *sharding.ShardManager

	// Mock tunnel server for agent connections
	MockTunnelServer *httptest.Server

	// Context for the test run
	ctx    context.Context
	cancel context.CancelFunc

	// Secret for Hub-mode cluster credentials
	ClusterSecretName string

	// Bootstrap token for Edge cluster tunnel authentication
	BootstrapToken string
}

var (
	testEnv     *TestEnvironment
	testEnvOnce sync.Once
	testEnvErr  error
)

// SetupTestEnvironment initializes the shared test environment.
// This should be called once at the beginning of the test suite.
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	testEnvOnce.Do(func() {
		testEnv, testEnvErr = setupTestEnvironmentInternal(t)
	})

	if testEnvErr != nil {
		t.Fatalf("Failed to setup test environment: %v", testEnvErr)
	}
	return testEnv
}

func setupTestEnvironmentInternal(t *testing.T) (*TestEnvironment, error) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	ctx, cancel := context.WithCancel(context.Background())

	cfg, err := ctrl.GetConfig()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	scheme := setupScheme()

	// Create client
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Create namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: TestNamespace}}
	_ = c.Create(ctx, ns)

	// Create Manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	// Setup components
	tunnelServer := handler.NewRemoteDialerServer(mgr.GetClient())
	clientManager := managercluster.NewClientManager(mgr.GetClient(), tunnelServer, TestNamespace)
	shardManager := sharding.NewShardManager(0, 1)

	// Setup controllers
	if err := (&application.ApplicationReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClientManager: clientManager,
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup application reconciler: %w", err)
	}

	if err := (&application.StatusReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClientManager: clientManager,
		ShardManager:  shardManager,
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup status reconciler: %w", err)
	}

	if err := (&managercluster.ClusterReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		HeartbeatTimeout: 1 * time.Minute,
		Namespace:        TestNamespace,
		ClientManager:    clientManager,
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup cluster reconciler: %w", err)
	}

	if err := (&addoncontroller.AddonReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Controllers: func() map[string]addon.AddonController {
			controllers := map[string]addon.AddonController{}
			for _, a := range addon.GetRegistry().List() {
				ctrl, err := a.ManagerController(mgr)
				if err != nil || ctrl == nil {
					continue
				}
				controllers[a.Name()] = ctrl
			}
			return controllers
		}(),
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup addon reconciler: %w", err)
	}

	if err := (&workspace.WorkspaceReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClientManager: clientManager,
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup workspace reconciler: %w", err)
	}

	// Setup scheduler
	schedCache := cache.NewCache()
	schedQueue := queue.NewSchedulingQueue()
	config := framework.DefaultSchedulerConfig()
	config.ScorePlugins = append(config.ScorePlugins, framework.PluginConfig{Name: "TopologySpread", Enabled: true, Weight: 1})
	sched := scheduler.NewSchedulerWithConfig(mgr.GetClient(), schedCache, schedQueue, config)

	if err := mgr.Add(&runnableFunc{fn: func(ctx context.Context) error {
		sched.Run(ctx)
		return nil
	}}); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to add scheduler: %w", err)
	}

	if err := (&scheduler.EventHandler{
		Client: mgr.GetClient(),
		Cache:  schedCache,
		Queue:  schedQueue,
	}).SetupWithManager(mgr); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup scheduler event handler: %w", err)
	}

	// Start manager in background
	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Printf("Manager failed: %v\n", err)
		}
	}()

	// Create bootstrap token for Edge cluster tunnel authentication
	// The token format is: [a-z0-9]{6}.[a-z0-9]{16}
	bootstrapTokenID := "abcdef"
	bootstrapTokenSecret := "1234567890abcdef"
	bootstrapToken := bootstrapTokenID + "." + bootstrapTokenSecret

	bootstrapTokenSecretObj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + bootstrapTokenID,
			Namespace: "kube-system",
		},
		Type: bootstrapapi.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			bootstrapapi.BootstrapTokenIDKey:               []byte(bootstrapTokenID),
			bootstrapapi.BootstrapTokenSecretKey:           []byte(bootstrapTokenSecret),
			bootstrapapi.BootstrapTokenUsageAuthentication: []byte("true"),
			bootstrapapi.BootstrapTokenExpirationKey:       []byte(time.Now().Add(24 * time.Hour).Format(time.RFC3339)),
		},
	}
	_ = c.Delete(ctx, bootstrapTokenSecretObj)
	if err := c.Create(ctx, bootstrapTokenSecretObj); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	// Create mock tunnel server
	mockTunnelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/connect" {
			tunnelServer.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	// Create cluster secret with credentials
	secretName := "e2e-cluster-secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: TestNamespace,
		},
		Data: map[string][]byte{},
	}
	if cfg.BearerToken != "" {
		secret.Data["token"] = []byte(cfg.BearerToken)
	}
	if cfg.TLSClientConfig.CAData != nil {
		secret.Data["caData"] = cfg.TLSClientConfig.CAData
	}
	if cfg.TLSClientConfig.CertData != nil {
		secret.Data["certData"] = cfg.TLSClientConfig.CertData
	}
	if cfg.TLSClientConfig.KeyData != nil {
		secret.Data["keyData"] = cfg.TLSClientConfig.KeyData
	}
	_ = c.Delete(ctx, secret)
	if err := c.Create(ctx, secret); err != nil {
		cancel()
		mockTunnelServer.Close()
		return nil, fmt.Errorf("failed to create cluster secret: %w", err)
	}

	// Wait for manager to be ready
	time.Sleep(2 * time.Second)

	return &TestEnvironment{
		Config:            cfg,
		Client:            c,
		Manager:           mgr,
		TunnelServer:      tunnelServer,
		ClientManager:     clientManager,
		SchedCache:        schedCache,
		SchedQueue:        schedQueue,
		Scheduler:         sched,
		ShardManager:      shardManager,
		MockTunnelServer:  mockTunnelServer,
		ctx:               ctx,
		cancel:            cancel,
		ClusterSecretName: secretName,
		BootstrapToken:    bootstrapToken,
	}, nil
}

// Cleanup cleans up the test environment
func (e *TestEnvironment) Cleanup() {
	if e.MockTunnelServer != nil {
		e.MockTunnelServer.Close()
	}
	if e.cancel != nil {
		e.cancel()
	}
}

// Context returns the test context
func (e *TestEnvironment) Context() context.Context {
	return e.ctx
}

// CreateHubCluster creates a Hub-mode ManagedCluster for testing
func (e *TestEnvironment) CreateHubCluster(t *testing.T, name string, labels map[string]string) *clusterv1alpha1.ManagedCluster {
	ctx := e.Context()

	// Create a unique secret for this cluster to avoid conflicts
	// (the controller deletes clusters that share the same SecretRef)
	secretName := fmt.Sprintf("cluster-secret-%s", name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: TestNamespace,
		},
		Data: map[string][]byte{},
	}
	if e.Config.BearerToken != "" {
		secret.Data["token"] = []byte(e.Config.BearerToken)
	}
	if e.Config.TLSClientConfig.CAData != nil {
		secret.Data["caData"] = e.Config.TLSClientConfig.CAData
	}
	if e.Config.TLSClientConfig.CertData != nil {
		secret.Data["certData"] = e.Config.TLSClientConfig.CertData
	}
	if e.Config.TLSClientConfig.KeyData != nil {
		secret.Data["keyData"] = e.Config.TLSClientConfig.KeyData
	}
	_ = e.Client.Delete(ctx, secret)
	if err := e.Client.Create(ctx, secret); err != nil {
		t.Logf("Warning: failed to create cluster secret %s: %v", secretName, err)
	}

	mc := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			APIServer:      e.Config.Host,
			SecretRef:      &corev1.LocalObjectReference{Name: secretName},
		},
	}

	// Delete if exists
	_ = e.Client.Delete(ctx, mc)
	wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &clusterv1alpha1.ManagedCluster{})
		return client.IgnoreNotFound(err) == nil && err != nil, nil
	})

	if err := e.Client.Create(ctx, mc); err != nil {
		t.Fatalf("Failed to create Hub cluster %s: %v", name, err)
	}

	// Wait for cluster controller to reconcile and set state to Ready first
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 15*time.Second, true, func(ctx context.Context) (bool, error) {
		var got clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &got); err != nil {
			return false, nil
		}
		return got.Status.State == clusterv1alpha1.ClusterReady, nil
	})
	if err != nil {
		t.Logf("Warning: cluster %s may not have been reconciled to Ready state", name)
	}

	// Update status with resources (after controller has reconciled)
	e.UpdateClusterStatus(t, name, "10", "10Gi")

	return mc
}

// CreateEdgeCluster creates an Edge-mode ManagedCluster with an agent for testing
func (e *TestEnvironment) CreateEdgeCluster(t *testing.T, name string, labels map[string]string) (*clusterv1alpha1.ManagedCluster, *cluster.Agent) {
	ctx := e.Context()
	mc := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
			APIServer:      e.Config.Host,
		},
	}

	// Delete if exists
	_ = e.Client.Delete(ctx, mc)
	wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &clusterv1alpha1.ManagedCluster{})
		return client.IgnoreNotFound(err) == nil && err != nil, nil
	})

	if err := e.Client.Create(ctx, mc); err != nil {
		t.Fatalf("Failed to create Edge cluster %s: %v", name, err)
	}

	// Create and configure agent
	// For e2e testing, we use the bootstrap token created during setup
	agent := cluster.NewAgent(cluster.AgentOptions{
		ClusterName:       name,
		TunnelURL:         e.MockTunnelServer.URL,
		HubURL:            e.Config.Host,
		BootstrapToken:    e.BootstrapToken, // Use the real bootstrap token from setup
		HeartbeatInterval: 2 * time.Second,
	})

	// Instead of agent.InitHubClient() which would fail with fake token,
	// we directly set the HubClient to use the test client
	agent.HubClient = e.Client
	agent.HubConfig = e.Config

	// Register the cluster
	if err := agent.Register(ctx); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Update cluster with credentials for the tunnel
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var got clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &got); err != nil {
			return false, nil
		}
		if got.Annotations == nil {
			got.Annotations = make(map[string]string)
		}
		// Set credentials annotations (base64 encoded where needed)
		if e.Config.BearerToken != "" {
			got.Annotations[constants.AnnotationCredentialsToken] = e.Config.BearerToken
		}
		if len(e.Config.CAData) > 0 {
			got.Annotations[constants.AnnotationCredentialsCA] = base64.StdEncoding.EncodeToString(e.Config.CAData)
		}
		if len(e.Config.CertData) > 0 {
			got.Annotations["cluster.rocket.io/credentials-cert"] = base64.StdEncoding.EncodeToString(e.Config.CertData)
		}
		if len(e.Config.KeyData) > 0 {
			got.Annotations["cluster.rocket.io/credentials-key"] = base64.StdEncoding.EncodeToString(e.Config.KeyData)
		}
		got.Annotations[constants.AnnotationAPIServerURL] = e.Config.Host
		got.Spec.APIServer = e.Config.Host
		return e.Client.Update(ctx, &got) == nil, nil
	})
	if err != nil {
		t.Fatalf("Failed to update Edge cluster credentials: %v", err)
	}

	// Start agent heartbeat in background (this updates LastKeepAliveTime)
	agentCtx, agentCancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-agentCtx.Done():
				return
			case <-ticker.C:
				// Send heartbeat
				var latest clusterv1alpha1.ManagedCluster
				if err := e.Client.Get(agentCtx, types.NamespacedName{Name: name}, &latest); err == nil {
					now := metav1.Now()
					latest.Status.LastKeepAliveTime = &now
					_ = e.Client.Status().Update(agentCtx, &latest)
				}
			}
		}
	}()

	// Register cleanup for agent
	t.Cleanup(func() {
		agentCancel()
	})

	// Start tunnel connection to the mock server
	go func() {
		// In a real scenario, this would establish a WebSocket connection
		// For e2e test, the tunnel server already handles requests through mockTunnelServer
		_ = agent.StartTunnel(agentCtx)
	}()

	// Update cluster status with resources
	e.UpdateClusterStatus(t, name, "10", "10Gi")

	// Wait for cluster to become Ready
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var got clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &got); err != nil {
			return false, nil
		}
		return got.Status.State == clusterv1alpha1.ClusterReady, nil
	})
	if err != nil {
		t.Fatalf("Edge cluster %s did not become Ready: %v", name, err)
	}

	return mc, agent
}

// UpdateClusterStatus updates a cluster's status with resources and marks it as Ready
func (e *TestEnvironment) UpdateClusterStatus(t *testing.T, name, cpu, memory string) {
	ctx := e.Context()
	// Update status with retry for conflict resolution - increased timeout for stability
	err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var latest clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &latest); err != nil {
			return false, nil
		}
		latest.Status.ResourceSummary = []clusterv1alpha1.ResourceSummary{
			{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
					corev1.ResourceMemory: resource.MustParse(memory),
				},
				Allocated: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("0"),
					corev1.ResourceMemory: resource.MustParse("0"),
				},
			},
		}
		latest.Status.State = clusterv1alpha1.ClusterReady
		return e.Client.Status().Update(ctx, &latest) == nil, nil
	})
	if err != nil {
		t.Logf("Failed to update cluster status for %s: %v", name, err)
		t.FailNow()
	}

	// Verify the status was actually set - increased timeout
	err = wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var latest clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &latest); err != nil {
			return false, nil
		}
		return latest.Status.State == clusterv1alpha1.ClusterReady && len(latest.Status.ResourceSummary) > 0, nil
	})
	if err != nil {
		t.Logf("Warning: cluster %s status may not have been properly set", name)
	}
}

// UpdateClusterStatusWithAllocation updates a cluster's status with specific resource allocation
func (e *TestEnvironment) UpdateClusterStatusWithAllocation(t *testing.T, name, allocatableCPU, allocatableMemory, allocatedCPU, allocatedMemory string) {
	ctx := e.Context()
	err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		var latest clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &latest); err != nil {
			return false, nil
		}
		latest.Status.ResourceSummary = []clusterv1alpha1.ResourceSummary{
			{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(allocatableCPU),
					corev1.ResourceMemory: resource.MustParse(allocatableMemory),
				},
				Allocated: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(allocatedCPU),
					corev1.ResourceMemory: resource.MustParse(allocatedMemory),
				},
			},
		}
		latest.Status.State = clusterv1alpha1.ClusterReady
		return e.Client.Status().Update(ctx, &latest) == nil, nil
	})
	if err != nil {
		t.Logf("Failed to update cluster status with allocation for %s: %v", name, err)
		t.FailNow()
	}
}

// DeleteCluster deletes a ManagedCluster
func (e *TestEnvironment) DeleteCluster(name string) {
	ctx := e.Context()
	mc := &clusterv1alpha1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_ = e.Client.Delete(ctx, mc)
}

// CreateApplication creates an Application for testing
func (e *TestEnvironment) CreateApplication(t *testing.T, app *appsv1alpha1.Application) {
	ctx := e.Context()
	// Delete if exists
	_ = e.Client.Delete(ctx, app)
	wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		err := e.Client.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, &appsv1alpha1.Application{})
		return client.IgnoreNotFound(err) == nil && err != nil, nil
	})

	// Also delete underlying workload if exists (to avoid immutable field errors)
	switch app.Spec.Workload.Kind {
	case "Deployment":
		deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, deploy)
	case "StatefulSet":
		sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, sts)
	case "DaemonSet":
		ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, ds)
	case "Job":
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, job)
	case "CronJob":
		cj := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, cj)
	}
	// Also delete PDB if resiliency is set
	if app.Spec.Resiliency != nil {
		pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
		_ = e.Client.Delete(ctx, pdb)
	}
	// Wait a bit for deletion to propagate
	time.Sleep(500 * time.Millisecond)

	if err := e.Client.Create(ctx, app); err != nil {
		t.Fatalf("Failed to create application %s: %v", app.Name, err)
	}
}

// DeleteApplication deletes an Application
func (e *TestEnvironment) DeleteApplication(name, namespace string) {
	ctx := e.Context()
	app := &appsv1alpha1.Application{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	_ = e.Client.Delete(ctx, app)
}

// WaitForApplicationScheduled waits for an application to be scheduled
func (e *TestEnvironment) WaitForApplicationScheduled(t *testing.T, name, namespace string, timeout time.Duration) *appsv1alpha1.Application {
	ctx := e.Context()
	var app appsv1alpha1.Application
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &app); err != nil {
			return false, nil
		}
		return len(app.Status.Placement.Topology) > 0, nil
	})
	if err != nil {
		t.Fatalf("Application %s/%s was not scheduled within %v", namespace, name, timeout)
	}
	return &app
}

// WaitForClusterState waits for a cluster to reach a specific state
func (e *TestEnvironment) WaitForClusterState(t *testing.T, name string, state clusterv1alpha1.ClusterState, timeout time.Duration) *clusterv1alpha1.ManagedCluster {
	ctx := e.Context()
	var cluster clusterv1alpha1.ManagedCluster
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &cluster); err != nil {
			return false, nil
		}
		return cluster.Status.State == state, nil
	})
	if err != nil {
		t.Fatalf("Cluster %s did not reach state %s within %v", name, state, timeout)
	}
	return &cluster
}

// WaitForClusterSecret waits for the Edge credentials secret to be created
func (e *TestEnvironment) WaitForClusterSecret(t *testing.T, clusterName string, timeout time.Duration) *corev1.Secret {
	ctx := e.Context()
	secretName := fmt.Sprintf("cluster-creds-%s", clusterName)
	var secret corev1.Secret
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		if err := e.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: TestNamespace}, &secret); err != nil {
			return false, nil
		}
		return len(secret.Data) > 0, nil
	})
	if err != nil {
		t.Fatalf("Cluster secret %s was not created within %v", secretName, timeout)
	}
	return &secret
}

// PatchClusterAnnotations patches annotations on a ManagedCluster
func (e *TestEnvironment) PatchClusterAnnotations(t *testing.T, name string, annotations map[string]string) {
	ctx := e.Context()
	err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		var cluster clusterv1alpha1.ManagedCluster
		if err := e.Client.Get(ctx, types.NamespacedName{Name: name}, &cluster); err != nil {
			return false, nil
		}
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		for k, v := range annotations {
			cluster.Annotations[k] = v
		}
		return e.Client.Update(ctx, &cluster) == nil, nil
	})
	if err != nil {
		t.Fatalf("Failed to patch annotations for cluster %s: %v", name, err)
	}
}

// CreateClusterSecret creates a secret in the test namespace for hub cluster auth
func (e *TestEnvironment) CreateClusterSecret(t *testing.T, name string) *corev1.Secret {
	ctx := e.Context()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestNamespace,
		},
		Data: map[string][]byte{},
	}
	if e.Config.BearerToken != "" {
		secret.Data["token"] = []byte(e.Config.BearerToken)
	}
	if e.Config.TLSClientConfig.CAData != nil {
		secret.Data["caData"] = e.Config.TLSClientConfig.CAData
	}
	if e.Config.TLSClientConfig.CertData != nil {
		secret.Data["certData"] = e.Config.TLSClientConfig.CertData
	}
	if e.Config.TLSClientConfig.KeyData != nil {
		secret.Data["keyData"] = e.Config.TLSClientConfig.KeyData
	}
	_ = e.Client.Delete(ctx, secret)
	if err := e.Client.Create(ctx, secret); err != nil {
		t.Fatalf("Failed to create cluster secret %s: %v", name, err)
	}
	return secret
}

// runnableFunc wraps a function to implement the Runnable interface
type runnableFunc struct {
	fn func(context.Context) error
}

func (r *runnableFunc) Start(ctx context.Context) error {
	return r.fn(ctx)
}
