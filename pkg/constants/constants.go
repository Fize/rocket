package constants

const (
	// Annotation keys for cluster credentials
	AnnotationCredentialsCA    = "cluster.rocket.io/credentials-ca"
	AnnotationCredentialsToken = "cluster.rocket.io/credentials-token"
	AnnotationAPIServerURL     = "cluster.rocket.io/apiserver-url"
	AnnotationCredentialsCert  = "cluster.rocket.io/credentials-cert"
	AnnotationCredentialsKey   = "cluster.rocket.io/credentials-key"

	// Defaults
	DefaultNamespace           = "rocket-system"
	DefaultAPIServerURL        = "https://kubernetes.default.svc:443"
	DefaultKubeSystemNamespace = "kube-system"
)
