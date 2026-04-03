/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"
	"time"

	"github.com/hex-techs/rocket/internal/agent/addon"
	"github.com/hex-techs/rocket/internal/agent/cluster"
	_ "github.com/hex-techs/rocket/internal/addon/kruiserollout"
	_ "github.com/hex-techs/rocket/internal/addon/mcs"
	_ "github.com/hex-techs/rocket/internal/addon/victoriametrics"
	"github.com/hex-techs/rocket/pkg/scheme"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	setupLog   = ctrl.Log.WithName("setup")
	_Namespace string
)

func init() {
	_Namespace = os.Getenv("NAMESPACE")
	if _Namespace == "" {
		_Namespace = "kube-system"
	}
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var hubURL string
	var tunnelURL string
	var clusterName string
	var bootstrapToken string
	var heartbeatInterval time.Duration

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.StringVar(&metricsAddr, "metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	pflag.StringVar(&probeAddr, "health-probe-bind-address", ":8091", "The address the probe endpoint binds to.")
	pflag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pflag.StringVar(&hubURL, "hub-url", "", "The URL of the Hub API Server")
	pflag.StringVar(&tunnelURL, "tunnel-url", "", "The URL of the Tunnel Server")
	pflag.StringVar(&clusterName, "cluster-name", "", "The name of this cluster in the Hub")
	pflag.StringVar(&bootstrapToken, "bootstrap-token", "", "The bootstrap token for authentication")
	pflag.DurationVar(&heartbeatInterval, "heartbeat-interval", 1*time.Minute, "The interval for sending heartbeats")
	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "74edaf61.hextech.io",
		LeaderElectionNamespace: _Namespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start agent")
		os.Exit(1)
	}

	setupLog.Info("starting agent manager")
	ctx := ctrl.SetupSignalHandler()

	// Initialize and run Cluster Agent if configured
	if clusterName != "" && tunnelURL != "" {
		setupLog.Info("Starting Cluster Agent...")
		clusterAgent := cluster.NewAgent(cluster.AgentOptions{
			HubURL:            hubURL,
			TunnelURL:         tunnelURL,
			ClusterName:       clusterName,
			BootstrapToken:    bootstrapToken,
			HeartbeatInterval: heartbeatInterval,
		})

		if err := clusterAgent.InitHubClient(); err != nil {
			setupLog.Error(err, "Failed to initialize hub client")
			os.Exit(1)
		}

		if err := clusterAgent.Register(ctx); err != nil {
			setupLog.Error(err, "Failed to register agent")
			os.Exit(1)
		}

		go func() {
			if err := clusterAgent.StartHeartbeat(ctx); err != nil {
				setupLog.Error(err, "Heartbeat loop failed")
			}
		}()

		go func() {
			if err := clusterAgent.StartTunnel(ctx); err != nil {
				setupLog.Error(err, "Tunnel failed")
			}
		}()

		// Setup addon reconciler to watch ManagedCluster from Hub
		if err := (&addon.AddonReconciler{
			HubClient:   clusterAgent.HubClient,
			Scheme:      scheme.Scheme,
			ClusterName: clusterName,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create addon reconciler")
			os.Exit(1)
		}
		setupLog.Info("Addon reconciler initialized")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
