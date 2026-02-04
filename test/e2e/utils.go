//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = storagev1alpha1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)
	return scheme
}

func newClient(t *testing.T) client.Client {
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("failed get kubeconfig: %v", err)
	}

	scheme := setupScheme()
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return c
}

func toRaw(obj interface{}) runtime.RawExtension {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return runtime.RawExtension{Raw: b}
}
