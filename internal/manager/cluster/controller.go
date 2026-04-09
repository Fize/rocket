package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hex-techs/rocket/internal/manager/metrics"
	"github.com/hex-techs/rocket/pkg/constants"
	"github.com/hex-techs/rocket/pkg/observability"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ClusterReconciler reconciles a ManagedCluster object
type ClusterReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	HeartbeatTimeout time.Duration
	Namespace        string
	ClientManager    *ClientManager
}

// findClusterBySecretRef searches for an existing Cluster that uses the given secret name.
func (r *ClusterReconciler) findClusterBySecretRef(ctx context.Context, secretName, excludeName string) (*clusterv1alpha1.ManagedCluster, error) {
	list := &clusterv1alpha1.ManagedClusterList{}
	if err := r.List(ctx, list); err != nil {
		return nil, err
	}
	for _, c := range list.Items {
		if c.Name == excludeName {
			continue
		}
		if c.Spec.SecretRef != nil && c.Spec.SecretRef.Name == secretName {
			copy := c
			return &copy, nil
		}
	}
	return nil, nil
}

// +kubebuilder:rbac:groups=storage.rocket.io,resources=managedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.rocket.io,resources=managedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster clusterv1alpha1.ManagedCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		metrics.RemoveClusterMetrics(req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := observability.Tracer().Start(ctx, "ClusterReconcile",
		trace.WithAttributes(
			attribute.String("cluster.name", cluster.Name),
			attribute.String("cluster.mode", string(cluster.Spec.ConnectionMode)),
		),
	)
	defer span.End()

	// Update managed cluster total count
	clusterList := &clusterv1alpha1.ManagedClusterList{}
	if err := r.List(ctx, clusterList); err == nil {
		metrics.SetManagedClusterTotal(len(clusterList.Items))
	}

	// Clean up client cache when cluster is updated
	if r.ClientManager != nil {
		r.ClientManager.RemoveClient(cluster.Name)
	}

	mode := cluster.Spec.ConnectionMode
	if mode == "" {
		mode = clusterv1alpha1.ClusterConnectionModeHub
	}

	if mode == clusterv1alpha1.ClusterConnectionModeHub {
		return r.reconcileHub(ctx, &cluster)
	}
	return r.reconcileEdge(ctx, &cluster)
}

func (r *ClusterReconciler) reconcileHub(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) (ctrl.Result, error) {
	if cluster.Spec.SecretRef == nil || cluster.Spec.SecretRef.Name == "" {
		if cluster.Status.State != clusterv1alpha1.ClusterRejected {
			cluster.Status.State = clusterv1alpha1.ClusterRejected
			metrics.SetClusterConnectionState(cluster.Name, false)
			return ctrl.Result{}, r.updateStatus(ctx, cluster)
		}
		return ctrl.Result{}, nil
	}

	if cluster.Status.State == "" || cluster.Status.State == clusterv1alpha1.ClusterPending {
		if existing, err := r.findClusterBySecretRef(ctx, cluster.Spec.SecretRef.Name, cluster.Name); err == nil && existing != nil {
			if existing.Status.ID == "" {
				existing.Status.ID = uuid.New().String()
			}
			existing.Status.State = clusterv1alpha1.ClusterReady
			if err := r.updateStatus(ctx, existing); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Delete(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		if cluster.Status.ID == "" {
			cluster.Status.ID = uuid.New().String()
		}
		cluster.Status.State = clusterv1alpha1.ClusterReady
		metrics.SetClusterConnectionState(cluster.Name, true)
		if err := r.updateStatus(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) reconcileEdge(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) (ctrl.Result, error) {
	// 1. Handle Credentials from Annotations
	if cluster.Annotations != nil {
		hasCredentials := cluster.Annotations[constants.AnnotationCredentialsToken] != "" ||
			cluster.Annotations[constants.AnnotationCredentialsCert] != "" ||
			cluster.Annotations[constants.AnnotationCredentialsCA] != ""
		if hasCredentials {
			if err := r.handleEdgeCredentials(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
			// After handling credentials, we update the object and return to re-reconcile
			return ctrl.Result{}, nil
		}
	}

	// 2. Ensure ID exists
	if cluster.Status.ID == "" {
		cluster.Status.ID = uuid.New().String()
		if err := r.updateStatus(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 3. Check Heartbeat
	if cluster.Status.LastKeepAliveTime != nil {
		heartbeatLatency := time.Since(cluster.Status.LastKeepAliveTime.Time)
		metrics.SetHeartbeatLatency(cluster.Name, heartbeatLatency)

		if heartbeatLatency > r.HeartbeatTimeout {
			if cluster.Status.State != clusterv1alpha1.ClusterOffline {
				cluster.Status.State = clusterv1alpha1.ClusterOffline
				metrics.SetClusterConnectionState(cluster.Name, false)
				ctrl.Log.Info("Cluster went offline", "cluster", cluster.Name, "last_heartbeat", cluster.Status.LastKeepAliveTime.Time)
				observability.SpanError(ctx, fmt.Errorf("heartbeat timeout: %v", heartbeatLatency))
				if err := r.updateStatus(ctx, cluster); err != nil {
					return ctrl.Result{}, err
				}
			}
		} else {
			// If we have a heartbeat and state is not Ready/Offline, it might be Pending
			// But handleEdgeCredentials should have set it to Ready
			if cluster.Status.State != clusterv1alpha1.ClusterReady && cluster.Status.State != clusterv1alpha1.ClusterOffline {
				cluster.Status.State = clusterv1alpha1.ClusterReady
				metrics.SetClusterConnectionState(cluster.Name, true)
				if err := r.updateStatus(ctx, cluster); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *ClusterReconciler) handleEdgeCredentials(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) error {
	caDataB64 := cluster.Annotations[constants.AnnotationCredentialsCA]
	token := cluster.Annotations[constants.AnnotationCredentialsToken]
	apiServerURL := cluster.Annotations[constants.AnnotationAPIServerURL]
	certDataB64 := cluster.Annotations[constants.AnnotationCredentialsCert]
	keyDataB64 := cluster.Annotations[constants.AnnotationCredentialsKey]

	ctrl.Log.Info("Handling edge credentials", "cluster", cluster.Name, "apiServerURL", apiServerURL)

	caData, _ := base64.StdEncoding.DecodeString(caDataB64)
	certData, _ := base64.StdEncoding.DecodeString(certDataB64)
	keyData, _ := base64.StdEncoding.DecodeString(keyDataB64)

	secretName := fmt.Sprintf("cluster-creds-%s", cluster.Name)
	secretNamespace := r.Namespace
	if secretNamespace == "" {
		secretNamespace = constants.DefaultNamespace
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["caData"] = caData
		secret.Data["token"] = []byte(token)
		if len(certData) > 0 {
			secret.Data["certData"] = certData
		}
		if len(keyData) > 0 {
			secret.Data["keyData"] = keyData
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Update Cluster Status
	cluster.Status.APIServerURL = apiServerURL
	cluster.Status.State = clusterv1alpha1.ClusterReady
	metrics.SetClusterConnectionState(cluster.Name, true)
	if err := r.updateStatus(ctx, cluster); err != nil {
		return err
	}

	// Update Cluster Spec and Remove Annotations
	cluster.Spec.SecretRef = &corev1.LocalObjectReference{Name: secretName}
	delete(cluster.Annotations, constants.AnnotationCredentialsCA)
	delete(cluster.Annotations, constants.AnnotationCredentialsToken)
	delete(cluster.Annotations, constants.AnnotationAPIServerURL)
	delete(cluster.Annotations, constants.AnnotationCredentialsCert)
	delete(cluster.Annotations, constants.AnnotationCredentialsKey)

	return r.Update(ctx, cluster)
}

func (r *ClusterReconciler) updateStatus(ctx context.Context, cluster *clusterv1alpha1.ManagedCluster) error {
	return r.Status().Update(ctx, cluster)
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1alpha1.ManagedCluster{}).
		Complete(r)
}
