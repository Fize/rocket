package cluster

import (
	"context"
	"fmt"
	"net"
	"testing"

	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mockTunnelServer struct {
	hasSession bool
}

func (m *mockTunnelServer) HasSession(clientKey string) bool {
	return m.hasSession
}

func (m *mockTunnelServer) Dialer(clientKey string) remotedialer.Dialer {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, fmt.Errorf("not implemented")
	}
}

func TestClientManager_GetClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)

	ns := "rocket-system"

	// Mock ClientCreator to avoid actual rest.Config usage
	mockClientCreator := func(config *rest.Config, options client.Options) (client.Client, error) {
		return fake.NewClientBuilder().WithScheme(scheme).Build(), nil
	}

	tests := []struct {
		name        string
		clusterName string
		objects     []client.Object
		connected   bool
		wantErr     bool
	}{
		{
			name:        "Hub Mode - Success",
			clusterName: "hub-cluster",
			objects: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-cluster"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
						APIServer:      "https://localhost:6443",
						SecretRef:      &corev1.LocalObjectReference{Name: "hub-secret"},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-secret", Namespace: ns},
					Data:       map[string][]byte{"token": []byte("test-token")},
				},
			},
			wantErr: false,
		},
		{
			name:        "Hub Mode - Missing Secret",
			clusterName: "hub-missing-secret",
			objects: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-missing-secret"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
						APIServer:      "https://localhost:6443",
						SecretRef:      &corev1.LocalObjectReference{Name: "non-existent"},
					},
				},
			},
			wantErr: true,
		},
		{
			name:        "Edge Mode - Connected",
			clusterName: "edge-connected",
			objects: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "edge-connected"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
					},
				},
			},
			connected: true,
			wantErr:   false,
		},
		{
			name:        "Edge Mode - Disconnected",
			clusterName: "edge-disconnected",
			objects: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "edge-disconnected"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
					},
				},
			},
			connected: false,
			wantErr:   true,
		},
		{
			name:        "Cluster Not Found",
			clusterName: "not-found",
			objects:     []client.Object{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hubClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			tunnelServer := &mockTunnelServer{hasSession: tt.connected}
			manager := NewClientManager(hubClient, tunnelServer, ns)
			manager.ClientCreator = mockClientCreator

			client, err := manager.GetClient(context.Background(), tt.clusterName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("GetClient() returned nil client without error")
			}
		})
	}
}
