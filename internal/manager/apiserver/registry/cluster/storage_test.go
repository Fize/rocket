package cluster

import (
	"context"
	nethttp "net/http"
	"testing"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/rancher/remotedialer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clusterv1alpha1.AddToScheme(scheme)
	_ = storagev1alpha1.AddToScheme(scheme)
	return scheme
}

func TestRESTCreate(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	ctx := request.WithNamespace(context.Background(), "")
	cluster := &clusterv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: clusterv1alpha1.ClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
		},
	}

	createdObj, err := r.Create(ctx, cluster, nil, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create cluster: %v", err)
	}

	createdCluster := createdObj.(*clusterv1alpha1.Cluster)
	if createdCluster.Name != cluster.Name {
		t.Errorf("expected name %s, got %s", cluster.Name, createdCluster.Name)
	}

	// Verify it exists in backend
	var mc storagev1alpha1.ManagedCluster
	if err := c.Get(ctx, client.ObjectKey{Name: "test-cluster"}, &mc); err != nil {
		t.Fatalf("failed to find managedcluster in backend: %v", err)
	}
	if string(mc.Spec.ConnectionMode) != string(cluster.Spec.ConnectionMode) {
		t.Errorf("expected connection mode %s, got %s", cluster.Spec.ConnectionMode, mc.Spec.ConnectionMode)
	}
}

func TestRESTGetList(t *testing.T) {
	scheme := setupScheme()
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeEdge,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()
	r := NewREST(c)

	ctx := context.Background()

	// Test Get
	gotObj, err := r.Get(ctx, "test-cluster", &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get cluster: %v", err)
	}
	gotCluster := gotObj.(*clusterv1alpha1.Cluster)
	if gotCluster.Name != "test-cluster" {
		t.Errorf("expected name test-cluster, got %s", gotCluster.Name)
	}

	// Test List
	listObj, err := r.List(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list clusters: %v", err)
	}
	clusterList := listObj.(*clusterv1alpha1.ClusterList)
	if len(clusterList.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(clusterList.Items))
	}
	if clusterList.Items[0].Name != "test-cluster" {
		t.Errorf("expected name test-cluster, got %s", clusterList.Items[0].Name)
	}
}

func TestRESTUpdate(t *testing.T) {
	scheme := setupScheme()
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cluster",
			ResourceVersion: "1",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeEdge,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()
	r := NewREST(c)

	ctx := context.Background()

	// Mock UpdateInfo
	updateInfo := &mockUpdatedObjectInfo{
		updatedObject: &clusterv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Spec: clusterv1alpha1.ClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			},
		},
	}

	updatedObj, created, err := r.Update(ctx, "test-cluster", updateInfo, nil, nil, false, &metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update cluster: %v", err)
	}
	if created {
		t.Errorf("expected created=false")
	}

	updatedCluster := updatedObj.(*clusterv1alpha1.Cluster)
	if updatedCluster.Spec.ConnectionMode != clusterv1alpha1.ClusterConnectionModeHub {
		t.Errorf("expected mode Hub, got %s", updatedCluster.Spec.ConnectionMode)
	}
}

func TestRESTDelete(t *testing.T) {
	scheme := setupScheme()
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()
	r := NewREST(c)

	ctx := context.Background()
	_, deleted, err := r.Delete(ctx, "test-cluster", nil, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete cluster: %v", err)
	}
	if !deleted {
		t.Errorf("expected deleted=true")
	}

	// Verify gone
	if err := c.Get(ctx, client.ObjectKey{Name: "test-cluster"}, &storagev1alpha1.ManagedCluster{}); err == nil {
		t.Error("expected managedcluster to be deleted from backend")
	}
}

type mockUpdatedObjectInfo struct {
	updatedObject runtime.Object
}

func (m *mockUpdatedObjectInfo) Preconditions() *metav1.Preconditions { return nil }
func (m *mockUpdatedObjectInfo) UpdatedObject(ctx context.Context, oldObj runtime.Object) (runtime.Object, error) {
	return m.updatedObject, nil
}

func TestProxyRESTConnect(t *testing.T) {
	scheme := setupScheme()
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeEdge,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mc).Build()

	// remotedialer.New takes an authorizer and error writer
	server := remotedialer.New(nil, nil)
	r := NewProxyREST(c, server)

	ctx := context.Background()
	handler, err := r.Connect(ctx, "test-cluster", nil, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	if handler == nil {
		t.Fatal("expected handler to be non-nil")
	}

	// Test case: cluster not found
	_, err = r.Connect(ctx, "non-existent", nil, nil)
	if err == nil {
		t.Error("expected error for non-existent cluster")
	}

	// Test case: Hub mode (currently not implemented)
	hubMc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hub-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeHub,
		},
	}
	_ = c.Create(ctx, hubMc)

	_, err = r.Connect(ctx, "hub-cluster", nil, nil)
	if err == nil || err.Error() != "Hub mode proxy not implemented" {
		t.Errorf("expected 'Hub mode proxy not implemented' error, got %v", err)
	}
}

func TestRESTNew(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	obj := r.New()
	if _, ok := obj.(*clusterv1alpha1.Cluster); !ok {
		t.Error("New() should return *Cluster")
	}
}

func TestRESTNewList(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	obj := r.NewList()
	if _, ok := obj.(*clusterv1alpha1.ClusterList); !ok {
		t.Error("NewList() should return *ClusterList")
	}
}

func TestRESTNamespaceScoped(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	if r.NamespaceScoped() {
		t.Error("Cluster should not be namespace scoped")
	}
}

func TestRESTGetSingularName(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	if r.GetSingularName() != "cluster" {
		t.Errorf("expected 'cluster', got %s", r.GetSingularName())
	}
}

func TestRESTDestroy(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	// Destroy should not panic
	r.Destroy()
}

func TestProxyRESTDestroy(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	server := remotedialer.New(nil, nil)
	r := NewProxyREST(c, server)

	// Destroy should not panic
	r.Destroy()
}

func TestProxyRESTNew(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	server := remotedialer.New(nil, nil)
	r := NewProxyREST(c, server)

	obj := r.New()
	if _, ok := obj.(*clusterv1alpha1.Cluster); !ok {
		t.Error("New() should return *Cluster")
	}
}

func TestProxyRESTNewConnectOptions(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	server := remotedialer.New(nil, nil)
	r := NewProxyREST(c, server)

	obj, subpath, extra := r.NewConnectOptions()
	if obj != nil {
		t.Error("expected nil options")
	}
	if subpath {
		t.Error("expected subpath to be false")
	}
	if extra != "" {
		t.Error("expected empty extra string")
	}
}

func TestProxyRESTConnectMethods(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	server := remotedialer.New(nil, nil)
	r := NewProxyREST(c, server)

	methods := r.ConnectMethods()
	expected := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

	if len(methods) != len(expected) {
		t.Errorf("expected %d methods, got %d", len(expected), len(methods))
	}

	for i, m := range expected {
		if methods[i] != m {
			t.Errorf("expected method %s, got %s", m, methods[i])
		}
	}
}

func TestRESTGetNotFound(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	ctx := context.Background()
	_, err := r.Get(ctx, "non-existent", &metav1.GetOptions{})
	if err == nil {
		t.Error("expected error for non-existent cluster")
	}
}

func TestRESTCreateInvalidType(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	ctx := context.Background()
	_, err := r.Create(ctx, &storagev1alpha1.ManagedCluster{}, nil, &metav1.CreateOptions{})
	if err == nil || err.Error() != "not of type Cluster" {
		t.Errorf("expected 'not of type Cluster' error, got %v", err)
	}
}

func TestRESTConvertToTable(t *testing.T) {
	scheme := setupScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := NewREST(c)

	ctx := context.Background()

	// Test single cluster
	cluster := &clusterv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Status: clusterv1alpha1.ClusterStatus{
			State:             "Ready",
			KubernetesVersion: "v1.28.0",
		},
	}

	table, err := r.ConvertToTable(ctx, cluster, nil)
	if err != nil {
		t.Fatalf("failed to convert to table: %v", err)
	}
	if len(table.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(table.Rows))
	}
	if len(table.ColumnDefinitions) != 4 {
		t.Errorf("expected 4 columns, got %d", len(table.ColumnDefinitions))
	}

	// Test cluster list
	clusterList := &clusterv1alpha1.ClusterList{
		Items: []clusterv1alpha1.Cluster{
			{ObjectMeta: metav1.ObjectMeta{Name: "c1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "c2"}},
		},
	}

	table, err = r.ConvertToTable(ctx, clusterList, nil)
	if err != nil {
		t.Fatalf("failed to convert list to table: %v", err)
	}
	if len(table.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(table.Rows))
	}
}

func TestConvertFunctions(t *testing.T) {
	// Test convertToCluster and convertToManagedCluster with full data
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeEdge,
			APIServer:      "https://api.example.com",
		},
		Status: storagev1alpha1.ManagedClusterStatus{
			State:             "Ready",
			KubernetesVersion: "v1.28.0",
			NodeSummary: []storagev1alpha1.NodeSummary{
				{Name: "default", TotalNum: 5, ReadyNum: 5},
			},
			ResourceSummary: []storagev1alpha1.ResourceSummary{
				{Name: "default"},
			},
		},
	}

	cluster := convertToCluster(mc)
	if cluster.Name != mc.Name {
		t.Errorf("expected name %s, got %s", mc.Name, cluster.Name)
	}
	if string(cluster.Spec.ConnectionMode) != string(mc.Spec.ConnectionMode) {
		t.Error("ConnectionMode mismatch")
	}
	if len(cluster.Status.NodeSummary) != 1 {
		t.Error("expected 1 NodeSummary")
	}
	if len(cluster.Status.ResourceSummary) != 1 {
		t.Error("expected 1 ResourceSummary")
	}

	// Convert back
	mc2 := convertToManagedCluster(cluster)
	if mc2.Name != cluster.Name {
		t.Errorf("expected name %s, got %s", cluster.Name, mc2.Name)
	}
	if len(mc2.Status.NodeSummary) != 1 {
		t.Error("expected 1 NodeSummary after convert back")
	}
}

func TestProxyErrorResponder(t *testing.T) {
	r := &proxyErrorResponder{}

	// Create a mock ResponseWriter
	recorder := &mockResponseWriter{headers: make(map[string][]string)}

	// Call with actual error
	testErr := nethttp.ErrAbortHandler
	r.Error(recorder, nil, testErr)

	if recorder.statusCode != nethttp.StatusBadGateway {
		t.Errorf("expected status %d, got %d", nethttp.StatusBadGateway, recorder.statusCode)
	}
}

type mockResponseWriter struct {
	statusCode int
	body       []byte
	headers    map[string][]string
}

func (m *mockResponseWriter) Header() nethttp.Header {
	return m.headers
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.body = append(m.body, b...)
	return len(b), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
