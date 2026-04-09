package scheme

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	kruiseapi "github.com/openkruise/kruise-api"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	aggregatedclusterv1alpha1 "github.com/fize/rocket/pkg/apis/cluster/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	workspacev1alpha1 "github.com/fize/rocket/pkg/apis/workspace/v1alpha1"
)

// Scheme is the default scheme for Rocket
var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(kruiseapi.AddToScheme(Scheme))
	utilruntime.Must(appsv1alpha1.AddToScheme(Scheme))
	utilruntime.Must(clusterv1alpha1.AddToScheme(Scheme))
	utilruntime.Must(aggregatedclusterv1alpha1.AddToScheme(Scheme))
	utilruntime.Must(workspacev1alpha1.AddToScheme(Scheme))
}
