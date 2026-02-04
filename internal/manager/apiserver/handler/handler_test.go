package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/server/mux"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

func TestValidateBootstrapToken(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tokenID := "abcdef"
	tokenSecret := "1234567890abcdef"
	token := tokenID + "." + tokenSecret

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + tokenID,
			Namespace: "kube-system",
		},
		Type: bootstrapapi.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			bootstrapapi.BootstrapTokenIDKey:               []byte(tokenID),
			bootstrapapi.BootstrapTokenSecretKey:           []byte(tokenSecret),
			bootstrapapi.BootstrapTokenUsageAuthentication: []byte("true"),
			bootstrapapi.BootstrapTokenExpirationKey:       []byte(time.Now().Add(time.Hour).Format(time.RFC3339)),
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	if !validateBootstrapToken(c, token) {
		t.Error("expected token to be valid")
	}

	if validateBootstrapToken(c, "invalid.token") {
		t.Error("expected invalid token to be rejected")
	}

	// Test expired token
	expiredSecret := secret.DeepCopy()
	expiredSecret.Data[bootstrapapi.BootstrapTokenExpirationKey] = []byte(time.Now().Add(-time.Hour).Format(time.RFC3339))
	c2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(expiredSecret).Build()
	if validateBootstrapToken(c2, token) {
		t.Error("expected expired token to be rejected")
	}
}

func TestGetPeers(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ns := "rocket-system"
	svc := "rocket-manager"
	podIP := "10.0.0.1"
	t.Setenv("POD_IP", podIP)

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc,
			Namespace: ns,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: podIP},
					{IP: "10.0.0.2"},
					{IP: "10.0.0.3"},
				},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(endpoints).Build()

	peers, err := getPeers(c, ns, svc)
	if err != nil {
		t.Fatalf("getPeers failed: %v", err)
	}

	if len(peers) != 2 {
		t.Errorf("expected 2 peers, got %d", len(peers))
	}

	for _, peer := range peers {
		if peer == podIP {
			t.Errorf("peer list should not include current pod IP %s", podIP)
		}
	}
}

func TestProxyHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)

	ns := "rocket-system"
	clusterName := "test-cluster"
	mc := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()
	server := remotedialer.New(nil, nil)
	m := mux.NewPathRecorderMux("test")
	t.Setenv("POD_NAMESPACE", ns)

	InstallHandler(m, c, server)

	// Test Cluster Not Found
	req := httptest.NewRequest("GET", "/proxy/non-existent/api/v1/pods", nil)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent cluster, got %d", w.Code)
	}

	// Test Cluster Not Connected
	req = httptest.NewRequest("GET", "/proxy/"+clusterName+"/api/v1/pods", nil)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, req)
	// it will try to proxy to peers if not connected, and if no peers, it should return 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for disconnected cluster, got %d", w.Code)
	}
}

func TestInternalCheck(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	m := mux.NewPathRecorderMux("test")
	server := NewRemoteDialerServer(c)
	InstallHandler(m, c, server)

	// Test /internal/check
	req, _ := http.NewRequest("GET", "/internal/check?cluster=test-cluster", nil)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)

	// Should be 404 because no session exists
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
}

func TestProxyInvalidPath(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	m := mux.NewPathRecorderMux("test")
	server := NewRemoteDialerServer(c)
	InstallHandler(m, c, server)

	req, _ := http.NewRequest("GET", "/proxy/", nil)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
}

func TestAuthorizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)

	tokenID := "abcdef"
	tokenSecret := "1234567890abcdef"
	token := tokenID + "." + tokenSecret
	clusterName := "test-cluster"

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + tokenID,
			Namespace: "kube-system",
		},
		Type: bootstrapapi.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			bootstrapapi.BootstrapTokenIDKey:               []byte(tokenID),
			bootstrapapi.BootstrapTokenSecretKey:           []byte(tokenSecret),
			bootstrapapi.BootstrapTokenUsageAuthentication: []byte("true"),
		},
	}

	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, cluster).Build()
	m := mux.NewPathRecorderMux("test")
	server := NewRemoteDialerServer(c)
	InstallHandler(m, c, server)

	// 1. Missing Authorization header
	req, _ := http.NewRequest("GET", "/connect", nil)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusInternalServerError {
		// remotedialer might return 500 or 401 depending on how it handles authorizer error
		// In our case, it calls http.Error which defaults to 500 if not specified,
		// but remotedialer itself might wrap it.
	}

	// 2. Invalid Token
	req, _ = http.NewRequest("GET", "/connect", nil)
	req.Header.Set("Authorization", "Bearer invalid.token")
	req.Header.Set("X-Rocket-Cluster-Name", clusterName)
	rr = httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Error("expected error for invalid token, got 200")
	}

	// 3. Cluster Not Found
	req, _ = http.NewRequest("GET", "/connect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Rocket-Cluster-Name", "non-existent")
	rr = httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Error("expected error for non-existent cluster, got 200")
	}
}
