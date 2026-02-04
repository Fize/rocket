package labels

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// ManagedByKey is the label key used to identify resources managed by Rocket
	ManagedByKey = "app.kubernetes.io/managed-by"
	// ManagedByValue is the label value used to identify resources managed by Rocket
	ManagedByValue = "rocket"
)

// AddManagedBy adds the managed-by label to the object
func AddManagedBy(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ManagedByKey] = ManagedByValue
	obj.SetLabels(labels)
}
