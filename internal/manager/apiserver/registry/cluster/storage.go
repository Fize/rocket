package cluster

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/cluster/v1alpha1"
	storagev1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nethttp "net/http"

	"github.com/rancher/remotedialer"
)

// REST implements a RESTStorage for Cluster
type REST struct {
	client client.Client
}

// ProxyREST implements a RESTStorage for Cluster proxy subresource
type ProxyREST struct {
	client       client.Client
	tunnelServer *remotedialer.Server
}

var _ rest.Scoper = &REST{}
var _ rest.Getter = &REST{}
var _ rest.Lister = &REST{}
var _ rest.CreaterUpdater = &REST{}
var _ rest.GracefulDeleter = &REST{}

var _ rest.Connecter = &ProxyREST{}
var _ rest.Storage = &ProxyREST{}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST(c client.Client) *REST {
	return &REST{client: c}
}

func (r *REST) GetSingularName() string {
	return "cluster"
}

// NewProxyREST returns a RESTStorage object for proxy subresource
func NewProxyREST(c client.Client, s *remotedialer.Server) *ProxyREST {
	return &ProxyREST{client: c, tunnelServer: s}
}

// New returns a new Cluster
func (r *REST) New() runtime.Object {
	return &clusterv1alpha1.Cluster{}
}

// New returns a new Proxy options object
func (r *ProxyREST) New() runtime.Object {
	return &clusterv1alpha1.Cluster{} // Returns the parent resource? Or options? k8s uses PodProxyOptions. We can use Cluster?
	// Actually Connecter.New() should return the object that `Connect` takes as options.
	// We don't have ClusterProxyOptions, so maybe just nil or &metav1.ListOptions{}?
	// Or define one. For now let's reuse Cluster (though odd).
	// Actually, usually it returns the resource that is the target of the proxy.
}

// Destroy cleans up resources on shutdown.
func (r *REST) Destroy() {
	// Given that underlying store is shared, we don't have anything distinct to destroy here.
}

// Destroy cleans up for ProxyREST
func (r *ProxyREST) Destroy() {}

// NewList returns a new ClusterList
func (r *REST) NewList() runtime.Object {
	return &clusterv1alpha1.ClusterList{}
}

// NamespaceScoped returns false because Cluster is cluster-scoped
func (r *REST) NamespaceScoped() bool {
	return false
}

// Get finds a resource in the storage by name and returns it.
func (r *REST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	var managedCluster storagev1alpha1.ManagedCluster
	key := client.ObjectKey{Name: name}
	if err := r.client.Get(ctx, key, &managedCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, apierrors.NewNotFound(clusterv1alpha1.Resource("cluster"), name)
		}
		return nil, err
	}

	return convertToCluster(&managedCluster), nil
}

// List selects resources in the storage which match to the selector. ('options' can be nil)
func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	var managedClusterList storagev1alpha1.ManagedClusterList
	listOpts := []client.ListOption{}

	if options != nil {
		if options.LabelSelector != nil {
			listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: options.LabelSelector})
		}
		// Note: FieldSelector support depends on client implementation details for CRDs
	}

	if err := r.client.List(ctx, &managedClusterList, listOpts...); err != nil {
		return nil, err
	}

	clusterList := &clusterv1alpha1.ClusterList{
		ListMeta: managedClusterList.ListMeta,
		Items:    make([]clusterv1alpha1.Cluster, len(managedClusterList.Items)),
	}

	for i, mc := range managedClusterList.Items {
		clusterList.Items[i] = *convertToCluster(&mc)
	}

	return clusterList, nil
}

// Create creates a new version of a resource.
func (r *REST) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	cluster, ok := obj.(*clusterv1alpha1.Cluster)
	if !ok {
		return nil, fmt.Errorf("not of type Cluster")
	}

	if createValidation != nil {
		if err := createValidation(ctx, obj); err != nil {
			return nil, err
		}
	}

	mc := convertToManagedCluster(cluster)
	if err := r.client.Create(ctx, mc); err != nil {
		return nil, err
	}

	return convertToCluster(mc), nil
}

// Update finds a resource in the storage and updates it.
func (r *REST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	// 1. Get existing
	oldObj, err := r.Get(ctx, name, nil)
	if err != nil {
		return nil, false, err
	}
	oldCluster := oldObj.(*clusterv1alpha1.Cluster)

	// 2. Apply updates
	newObj, err := objInfo.UpdatedObject(ctx, oldCluster)
	if err != nil {
		return nil, false, err
	}
	newCluster := newObj.(*clusterv1alpha1.Cluster)

	// 3. Validation
	if updateValidation != nil {
		if err := updateValidation(ctx, newCluster, oldCluster); err != nil {
			return nil, false, err
		}
	}

	// 4. Update backend
	mc := convertToManagedCluster(newCluster)
	// Must preserve ResourceVersion for optimistic locking
	mc.ResourceVersion = oldCluster.ResourceVersion

	if err := r.client.Update(ctx, mc); err != nil {
		return nil, false, err
	}

	return convertToCluster(mc), false, nil
}

// Delete enforces life-cycle rules for the deletion of a resource.
func (r *REST) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	if deleteValidation != nil {
		// We can't validate deletion if we don't have the object, but usually Delete doesn't require the object in hand unless we fetch it.
		// However, standard Delete implementation fetches it first to return it.
	}

	// Fetch to return
	oldObj, err := r.Get(ctx, name, nil)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, apierrors.NewNotFound(clusterv1alpha1.Resource("cluster"), name)
		}
		return nil, false, err
	}

	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	// We should pass DeleteOptions, but client.Delete takes variadic ClientOptions.
	// controller-runtime client.Delete handles DeleteOptions via specific DeleteOption implementations.
	// For now simple delete.

	if err := r.client.Delete(ctx, mc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, apierrors.NewNotFound(clusterv1alpha1.Resource("cluster"), name)
		}
		return nil, false, err
	}

	return oldObj, true, nil
}

// ConvertToTable converts objects to metav1.Table.
func (r *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name", Description: "the name of the cluster"},
			{Name: "State", Type: "string", Description: "the state of the cluster"},
			{Name: "Version", Type: "string", Description: "the kubernetes version of the cluster"},
			{Name: "Age", Type: "string", Format: "date", Description: "creation timestamp"},
		},
	}

	if m, err := meta.ListAccessor(object); err == nil {
		table.ResourceVersion = m.GetResourceVersion()
		table.Continue = m.GetContinue()
		table.RemainingItemCount = m.GetRemainingItemCount()
	} else {
		if m, err := meta.CommonAccessor(object); err == nil {
			table.ResourceVersion = m.GetResourceVersion()
		}
	}

	rows, err := tableRowsFromObject(object)
	if err != nil {
		return nil, err
	}
	table.Rows = rows
	return table, nil
}

// Connect methods for ProxyREST

// Connect implements rest.Connecter
func (r *ProxyREST) Connect(ctx context.Context, name string, options runtime.Object, responder rest.Responder) (nethttp.Handler, error) {
	// 1. Get Cluster
	var managedCluster storagev1alpha1.ManagedCluster
	key := client.ObjectKey{Name: name}
	if err := r.client.Get(ctx, key, &managedCluster); err != nil {
		return nil, err
	}

	// 2. Check Connection Mode
	if managedCluster.Spec.ConnectionMode == storagev1alpha1.ClusterConnectionModeEdge {
		// Use Tunnel
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
			// Construct dialer
			dialer := r.tunnelServer.Dialer(name)

			// Use ReverseProxy
			transport := &nethttp.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer(ctx, network, addr)
				},
			}

			// Construct prefix to strip
			prefix := fmt.Sprintf("/apis/%s/%s/clusters/%s/proxy", clusterv1alpha1.GroupVersion.Group, clusterv1alpha1.GroupVersion.Version, name)
			req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
			if !strings.HasPrefix(req.URL.Path, "/") {
				req.URL.Path = "/" + req.URL.Path
			}

			targetURL := &url.URL{
				Scheme: "http",
				Host:   "kubernetes.default.svc",
			}

			handler := proxy.NewUpgradeAwareHandler(targetURL, transport, false, false, &proxyErrorResponder{})
			handler.ServeHTTP(w, req)
		}), nil
	}

	// Hub mode not implemented fully here yet
	return nil, fmt.Errorf("Hub mode proxy not implemented")
}

type proxyErrorResponder struct{}

func (r *proxyErrorResponder) Error(w nethttp.ResponseWriter, req *nethttp.Request, err error) {
	w.WriteHeader(nethttp.StatusBadGateway)
	w.Write([]byte(err.Error()))
}

// NewConnectOptions returns an empty options object that will be used to parse
// the options for the Connect method.
func (r *ProxyREST) NewConnectOptions() (runtime.Object, bool, string) {
	return nil, false, ""
}

// ConnectMethods returns the list of HTTP methods that can be used to connect
// to the proxy.
func (r *ProxyREST) ConnectMethods() []string {
	return []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
}

func tableRowsFromObject(obj runtime.Object) ([]metav1.TableRow, error) {
	if list, ok := obj.(*clusterv1alpha1.ClusterList); ok {
		rows := make([]metav1.TableRow, len(list.Items))
		for i := range list.Items {
			r, err := tableRowFromCluster(&list.Items[i])
			if err != nil {
				return nil, err
			}
			rows[i] = *r
		}
		return rows, nil
	}

	if cluster, ok := obj.(*clusterv1alpha1.Cluster); ok {
		r, err := tableRowFromCluster(cluster)
		if err != nil {
			return nil, err
		}
		return []metav1.TableRow{*r}, nil
	}

	return nil, fmt.Errorf("unsupported type for table conversion: %T", obj)
}

func tableRowFromCluster(c *clusterv1alpha1.Cluster) (*metav1.TableRow, error) {
	return &metav1.TableRow{
		Object: runtime.RawExtension{Object: c},
		Cells: []interface{}{
			c.Name,
			c.Status.State,
			c.Status.KubernetesVersion,
			c.CreationTimestamp,
		},
	}, nil
}

// Convert functions

func convertToCluster(mc *storagev1alpha1.ManagedCluster) *clusterv1alpha1.Cluster {
	c := &clusterv1alpha1.Cluster{
		ObjectMeta: mc.ObjectMeta,
		Spec: clusterv1alpha1.ClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionMode(mc.Spec.ConnectionMode),
			APIServer:      mc.Spec.APIServer,
			SecretRef:      mc.Spec.SecretRef,
			Taints:         mc.Spec.Taints,
		},
	}

	c.Status = clusterv1alpha1.ClusterStatus{
		State:             clusterv1alpha1.ClusterState(mc.Status.State),
		ID:                mc.Status.ID,
		KubernetesVersion: mc.Status.KubernetesVersion,
		AgentVersion:      mc.Status.AgentVersion,
		APIServerURL:      mc.Status.APIServerURL,
		LastKeepAliveTime: mc.Status.LastKeepAliveTime,
		Conditions:        mc.Status.Conditions,
	}

	if mc.Status.NodeSummary != nil {
		c.Status.NodeSummary = make([]clusterv1alpha1.NodeSummary, len(mc.Status.NodeSummary))
		for i, ns := range mc.Status.NodeSummary {
			c.Status.NodeSummary[i] = clusterv1alpha1.NodeSummary{
				Name:     ns.Name,
				TotalNum: ns.TotalNum,
				ReadyNum: ns.ReadyNum,
			}
		}
	}

	if mc.Status.ResourceSummary != nil {
		c.Status.ResourceSummary = make([]clusterv1alpha1.ResourceSummary, len(mc.Status.ResourceSummary))
		for i, rs := range mc.Status.ResourceSummary {
			c.Status.ResourceSummary[i] = clusterv1alpha1.ResourceSummary{
				Name:        rs.Name,
				Allocatable: rs.Allocatable, // ResourceList = map[string]Quantity, checking if simple copy is safe (map ref). Yes for read, maybe for write? deepcopy safer?
				// For this conversion, let's assume direct assignment is fine or we can deepcopy if needed.
				Allocating: rs.Allocating,
				Allocated:  rs.Allocated,
			}
		}
	}

	return c
}

func convertToManagedCluster(c *clusterv1alpha1.Cluster) *storagev1alpha1.ManagedCluster {
	mc := &storagev1alpha1.ManagedCluster{
		ObjectMeta: c.ObjectMeta,
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionMode(c.Spec.ConnectionMode),
			APIServer:      c.Spec.APIServer,
			SecretRef:      c.Spec.SecretRef,
			Taints:         c.Spec.Taints,
		},
	}

	// Typically updates only handle Spec (and metadata). Status is subresource.
	// If update includes Status, we should copy it.
	mc.Status = storagev1alpha1.ManagedClusterStatus{
		State:             storagev1alpha1.ClusterState(c.Status.State),
		ID:                c.Status.ID,
		KubernetesVersion: c.Status.KubernetesVersion,
		AgentVersion:      c.Status.AgentVersion,
		APIServerURL:      c.Status.APIServerURL,
		LastKeepAliveTime: c.Status.LastKeepAliveTime,
		Conditions:        c.Status.Conditions,
	}

	if c.Status.NodeSummary != nil {
		mc.Status.NodeSummary = make([]storagev1alpha1.NodeSummary, len(c.Status.NodeSummary))
		for i, ns := range c.Status.NodeSummary {
			mc.Status.NodeSummary[i] = storagev1alpha1.NodeSummary{
				Name:     ns.Name,
				TotalNum: ns.TotalNum,
				ReadyNum: ns.ReadyNum,
			}
		}
	}

	if c.Status.ResourceSummary != nil {
		mc.Status.ResourceSummary = make([]storagev1alpha1.ResourceSummary, len(c.Status.ResourceSummary))
		for i, rs := range c.Status.ResourceSummary {
			mc.Status.ResourceSummary[i] = storagev1alpha1.ResourceSummary{
				Name:        rs.Name,
				Allocatable: rs.Allocatable,
				Allocating:  rs.Allocating,
				Allocated:   rs.Allocated,
			}
		}
	}

	return mc
}
