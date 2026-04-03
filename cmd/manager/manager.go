package main

import (
	"context"
	"flag"
	"os"
	"strings"
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	_ "github.com/hex-techs/rocket/internal/addon/kruiserollout"
	_ "github.com/hex-techs/rocket/internal/addon/mcs"
	addoncontroller "github.com/hex-techs/rocket/internal/manager/addon"
	"github.com/hex-techs/rocket/internal/manager/apiserver"
	"github.com/hex-techs/rocket/internal/manager/apiserver/handler"
	"github.com/hex-techs/rocket/internal/manager/application"
	"github.com/hex-techs/rocket/internal/manager/cluster"
	"github.com/hex-techs/rocket/internal/manager/scheduler"
	"github.com/hex-techs/rocket/internal/manager/scheduler/cache"
	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	"github.com/hex-techs/rocket/internal/manager/scheduler/queue"
	"github.com/hex-techs/rocket/internal/manager/sharding"
	"github.com/hex-techs/rocket/internal/manager/workspace"
	"github.com/hex-techs/rocket/pkg/scheme"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var heartbeatTimeout time.Duration
	var tunnelPort int
	var shardID int
	var totalShards int
	var disabledWebhook bool
	var disabledAPIServer bool
	var enabledControllers string
	var schedulerResourceStrategy string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.DurationVar(&heartbeatTimeout, "heartbeat-timeout", 5*time.Minute, "The duration after which an Edge cluster is considered Offline")
	flag.IntVar(&tunnelPort, "tunnel-port", 443, "The port the tunnel server binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&shardID, "shard-id", 0, "The ID of the current shard (0-based).")
	flag.IntVar(&totalShards, "total-shards", 1, "The total number of shards.")
	flag.BoolVar(&disabledWebhook, "disabled-webhook", false, "Disable webhook server")
	flag.BoolVar(&disabledAPIServer, "disabled-apiserver", false, "Disable aggregated apiserver")
	flag.StringVar(&enabledControllers, "enabled-controllers", "", "The controllers to enable (comma separated). Empty means all.")
	flag.StringVar(&schedulerResourceStrategy, "scheduler-resource-strategy", "LeastAllocated", "The resource strategy to use for scheduling (LeastAllocated or MostAllocated).")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "application-manager.rocket.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Create Tunnel Server
	tunnelServer := handler.NewRemoteDialerServer(mgr.GetClient())

	// Create Client Manager
	clientManager := cluster.NewClientManager(mgr.GetClient(), tunnelServer, os.Getenv("POD_NAMESPACE"))

	// Create Shard Manager
	shardManager := sharding.NewShardManager(shardID, totalShards)

	isControllerEnabled := func(name string) bool {
		if enabledControllers == "" {
			return true
		}
		// extremely simple check
		return contains(enabledControllers, name)
	}

	if isControllerEnabled("Application") {
		if err = (&application.ApplicationReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			ClientManager: clientManager,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Application")
			os.Exit(1)
		}
	}

	if isControllerEnabled("ApplicationStatus") {
		if err = (&application.StatusReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			ClientManager: clientManager,
			ShardManager:  shardManager,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ApplicationStatus")
			os.Exit(1)
		}
	}

	if isControllerEnabled("Cluster") {
		if err = (&cluster.ClusterReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			HeartbeatTimeout: heartbeatTimeout,
			Namespace:        os.Getenv("POD_NAMESPACE"),
			ClientManager:    clientManager,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Cluster")
			os.Exit(1)
		}
	}

	if isControllerEnabled("Workspace") {
		if err = (&workspace.WorkspaceReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			ClientManager: clientManager,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Workspace")
			os.Exit(1)
		}
	}

	if isControllerEnabled("Addon") {
		if err = (&addoncontroller.AddonReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Addon")
			os.Exit(1)
		}
	}

	// Add Tunnel Server
	if !disabledAPIServer {
		if err := mgr.Add(&apiserver.APIServer{
			Client:       mgr.GetClient(),
			Port:         tunnelPort,
			TunnelServer: tunnelServer,
			Scheme:       mgr.GetScheme(),
		}); err != nil {
			setupLog.Error(err, "unable to add tunnel server to manager")
			os.Exit(1)
		}
	}

	// Setup Scheduler
	schedCache := cache.NewCache()
	schedQueue := queue.NewSchedulingQueue()

	schedConfig := framework.DefaultSchedulerConfig()
	if schedulerResourceStrategy != "" {
		for i, plugin := range schedConfig.ScorePlugins {
			if plugin.Name == "Resource" {
				if schedConfig.ScorePlugins[i].Args == nil {
					schedConfig.ScorePlugins[i].Args = make(map[string]interface{})
				}
				schedConfig.ScorePlugins[i].Args["strategy"] = schedulerResourceStrategy
				break
			}
		}
	}

	sched := scheduler.NewSchedulerWithConfig(mgr.GetClient(), schedCache, schedQueue, schedConfig)

	// Add Scheduler Runner as a Runnable
	if isControllerEnabled("Scheduler") {
		if err := mgr.Add(&schedulerRunnable{scheduler: sched}); err != nil {
			setupLog.Error(err, "unable to add scheduler to manager")
			os.Exit(1)
		}

		// Setup Scheduler Event Handlers (via Controller)
		// We need to watch Applications and Clusters to update Queue and Cache
		if err := (&scheduler.EventHandler{
			Client: mgr.GetClient(),
			Cache:  schedCache,
			Queue:  schedQueue,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create scheduler event handler")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// schedulerRunnable implements manager.Runnable interface
type schedulerRunnable struct {
	scheduler *scheduler.Scheduler
}

func (r *schedulerRunnable) Start(ctx context.Context) error {
	r.scheduler.Run(ctx)
	return nil
}

func contains(s, substr string) bool {
	parts := strings.Split(s, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == substr {
			return true
		}
	}
	return false
}
