package cluster

import (
	"context"
	"fmt"
	"net"
	"sync"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1alpha1.AddToScheme(scheme))
}

// TunnelServer defines the interface for remotedialer.Server
type TunnelServer interface {
	HasSession(clientKey string) bool
	Dialer(clientKey string) remotedialer.Dialer
}

// ClientManager manages clients for clusters
type ClientManager struct {
	HubClient    client.Client
	TunnelServer TunnelServer
	Namespace    string

	mu       sync.RWMutex
	clusters map[string]cluster.Cluster
	cancels  map[string]context.CancelFunc

	// ClientCreator is used to create new clients. Defaults to client.New.
	// Useful for testing.
	ClientCreator func(config *rest.Config, options client.Options) (client.Client, error)
}

// NewClientManager creates a new ClientManager
func NewClientManager(hubClient client.Client, tunnelServer TunnelServer, namespace string) *ClientManager {
	return &ClientManager{
		HubClient:     hubClient,
		TunnelServer:  tunnelServer,
		Namespace:     namespace,
		clusters:      make(map[string]cluster.Cluster),
		cancels:       make(map[string]context.CancelFunc),
		ClientCreator: client.New,
	}
}

// RemoveClient removes the client for the given cluster from the cache
func (m *ClientManager) RemoveClient(clusterName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, ok := m.cancels[clusterName]; ok {
		cancel()
		delete(m.cancels, clusterName)
	}
	delete(m.clusters, clusterName)
}

// GetCluster returns a cluster for the given cluster name
func (m *ClientManager) GetCluster(ctx context.Context, clusterName string) (cluster.Cluster, error) {
	m.mu.RLock()
	c, ok := m.clusters[clusterName]
	m.mu.RUnlock()
	if ok {
		return c, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check
	if c, ok := m.clusters[clusterName]; ok {
		return c, nil
	}

	// Get Cluster object
	clusterObj := &clusterv1alpha1.ManagedCluster{}
	if err := m.HubClient.Get(ctx, client.ObjectKey{Name: clusterName}, clusterObj); err != nil {
		return nil, fmt.Errorf("failed to get cluster %s: %w", clusterName, err)
	}

	var cfg *rest.Config
	var err error

	if clusterObj.Spec.ConnectionMode == clusterv1alpha1.ClusterConnectionModeHub {
		cfg, err = m.buildHubModeConfig(ctx, clusterObj)
	} else {
		cfg, err = m.buildEdgeModeConfig(ctx, clusterObj)
	}

	if err != nil {
		return nil, err
	}

	// Create cluster
	c, err = cluster.New(cfg, func(o *cluster.Options) {
		o.Scheme = scheme
		o.NewClient = m.ClientCreator
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster accessor for %s: %w", clusterName, err)
	}

	// Start cluster in background
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := c.Start(ctx); err != nil {
			log.Log.Error(err, "Cluster stopped with error", "cluster", clusterName)
		}
	}()

	// Wait for cache to sync?
	// Usually we want to wait, but GetCluster might be called in reconcile loop.
	// If we wait here, it might block.
	// But if we don't wait, the first client call might fail or wait.
	// controller-runtime client waits for cache sync automatically.

	m.clusters[clusterName] = c
	m.cancels[clusterName] = cancel

	return c, nil
}

// GetClient returns a client for the given cluster
func (m *ClientManager) GetClient(ctx context.Context, clusterName string) (client.Client, error) {
	c, err := m.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	return c.GetClient(), nil
}

func (m *ClientManager) buildHubModeConfig(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) (*rest.Config, error) {
	if cluster.Spec.SecretRef == nil {
		return nil, fmt.Errorf("missing secretRef for hub mode cluster %s", cluster.Name)
	}

	cfg := &rest.Config{
		Host: cluster.Spec.APIServer,
	}

	if err := m.loadCredentials(ctx, cluster.Spec.SecretRef.Name, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (m *ClientManager) buildEdgeModeConfig(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) (*rest.Config, error) {
	if !m.TunnelServer.HasSession(cluster.Name) {
		return nil, fmt.Errorf("cluster %s is not connected via tunnel", cluster.Name)
	}

	dialer := m.TunnelServer.Dialer(cluster.Name)
	host := cluster.Spec.APIServer
	if host == "" {
		host = "https://kubernetes.default.svc:443"
	}

	cfg := &rest.Config{
		Host: host,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer(ctx, network, addr)
		},
	}

	if cluster.Spec.SecretRef != nil {
		if err := m.loadCredentials(ctx, cluster.Spec.SecretRef.Name, cfg); err != nil {
			return nil, err
		}
	} else {
		// Default to insecure if no credentials provided for edge mode
		cfg.Insecure = true
	}

	return cfg, nil
}

func (m *ClientManager) loadCredentials(ctx context.Context, secretName string, cfg *rest.Config) error {
	secret := &corev1.Secret{}
	if err := m.HubClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: m.Namespace}, secret); err != nil {
		return fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	if caData, ok := secret.Data["caData"]; ok && len(caData) > 0 {
		cfg.CAData = caData
	} else {
		cfg.Insecure = true
	}
	if certData, ok := secret.Data["certData"]; ok && len(certData) > 0 {
		cfg.CertData = certData
	}
	if keyData, ok := secret.Data["keyData"]; ok && len(keyData) > 0 {
		cfg.KeyData = keyData
	}
	if token, ok := secret.Data["token"]; ok && len(token) > 0 {
		cfg.BearerToken = string(token)
	}

	// If no credentials are available, mark as insecure
	if cfg.BearerToken == "" && len(cfg.CertData) == 0 {
		cfg.Insecure = true
	}

	return nil
}
