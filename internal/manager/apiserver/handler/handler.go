package handler

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/rancher/remotedialer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/server/mux"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	bootstraputil "k8s.io/cluster-bootstrap/token/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NewRemoteDialerServer(c client.Client) *remotedialer.Server {
	logger := log.Log.WithName("tunnel")

	authorizer := func(req *http.Request) (string, bool, error) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			logger.Info("Missing authorization header", "remoteAddr", req.RemoteAddr)
			return "", false, fmt.Errorf("authorization header required")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			logger.Info("Invalid authorization header format", "remoteAddr", req.RemoteAddr)
			return "", false, fmt.Errorf("invalid authorization header format")
		}
		token := parts[1]

		if !validateBootstrapToken(c, token) {
			logger.Info("Invalid bootstrap token", "remoteAddr", req.RemoteAddr)
			return "", false, fmt.Errorf("invalid bootstrap token")
		}

		clusterName := req.Header.Get("X-Remotedialer-ID")
		if clusterName == "" {
			clusterName = req.Header.Get("X-Rocket-Cluster-Name")
		}
		if clusterName == "" {
			logger.Info("Missing cluster name", "remoteAddr", req.RemoteAddr)
			return "", false, fmt.Errorf("cluster name required")
		}

		cluster := &clusterv1alpha1.ManagedCluster{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: clusterName}, cluster); err != nil {
			logger.Info("Cluster not found", "cluster", clusterName, "error", err)
			return "", false, fmt.Errorf("cluster not found")
		}

		logger.Info("Cluster authorized successfully", "cluster", clusterName)
		return clusterName, true, nil
	}

	errorWriter := func(rw http.ResponseWriter, req *http.Request, code int, err error) {
		logger.Error(err, "remotedialer error", "code", code, "path", req.URL.Path)
		http.Error(rw, fmt.Sprintf("tunnel error: %v", err), code)
	}

	return remotedialer.New(authorizer, errorWriter)
}

func InstallHandler(m *mux.PathRecorderMux, c client.Client, server *remotedialer.Server) {
	logger := log.Log.WithName("tunnel")

	m.Handle("/connect", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Handling /connect request", "method", r.Method, "remoteAddr", r.RemoteAddr)
		server.ServeHTTP(w, r)
		logger.Info("Finished handling /connect request", "remoteAddr", r.RemoteAddr)
	}))

	m.Handle("/internal/check", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clusterName := r.URL.Query().Get("cluster")
		if server.HasSession(clusterName) {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	m.HandlePrefix("/proxy/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := os.Getenv("POD_NAMESPACE")
		if ns == "" {
			ns = "rocket-system"
		}

		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/proxy/"), "/", 2)
		if len(parts) < 1 {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		clusterName := parts[0]
		targetPath := "/"
		if len(parts) > 1 {
			targetPath = "/" + parts[1]
		}

		// Get Cluster info
		cluster := &clusterv1alpha1.ManagedCluster{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: clusterName}, cluster); err != nil {
			http.Error(w, fmt.Sprintf("Cluster %s not found", clusterName), http.StatusNotFound)
			return
		}

		if server.HasSession(clusterName) {
			// Load credentials if available
			var caData []byte
			var token string
			if cluster.Spec.SecretRef != nil {
				secret := &corev1.Secret{}
				err := c.Get(context.Background(), client.ObjectKey{
					Name:      cluster.Spec.SecretRef.Name,
					Namespace: ns,
				}, secret)
				if err == nil {
					caData = secret.Data["caData"]
					token = string(secret.Data["token"])
				}
			}

			targetURL := cluster.Status.APIServerURL
			if targetURL == "" {
				targetURL = "https://kubernetes.default.svc:443"
			}
			u, _ := url.Parse(targetURL)

			d := server.Dialer(clusterName)
			dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
				return d(ctx, network, addr)
			}

			tlsConfig := &tls.Config{InsecureSkipVerify: true}
			if len(caData) > 0 {
				caPool := x509.NewCertPool()
				if caPool.AppendCertsFromPEM(caData) {
					tlsConfig = &tls.Config{
						RootCAs: caPool,
					}
				}
			}

			rp := &httputil.ReverseProxy{
				Director: func(req *http.Request) {
					req.URL.Scheme = u.Scheme
					req.URL.Host = u.Host
					req.URL.Path = targetPath
					req.RequestURI = ""
					if token != "" {
						req.Header.Set("Authorization", "Bearer "+token)
					}
				},
				Transport: &http.Transport{
					DialContext:     dialer,
					TLSClientConfig: tlsConfig,
				},
			}
			rp.ServeHTTP(w, r)
			return
		}

		// Peer proxy logic
		peers, err := getPeers(c, ns, "rocket-apiserver")
		if err == nil {
			client := &http.Client{
				Timeout: 2 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}

			for _, peerIP := range peers {
				url := fmt.Sprintf("https://%s:443/internal/check?cluster=%s", peerIP, clusterName)
				resp, err := client.Get(url)
				if err == nil && resp.StatusCode == http.StatusOK {
					rp := &httputil.ReverseProxy{
						Director: func(req *http.Request) {
							req.URL.Scheme = "https"
							req.URL.Host = peerIP + ":443"
						},
						Transport: &http.Transport{
							TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
						},
					}
					rp.ServeHTTP(w, r)
					return
				}
			}
		}

		http.Error(w, fmt.Sprintf("Cluster %s not connected", clusterName), http.StatusServiceUnavailable)
	}))
}

func getPeers(c client.Client, namespace, serviceName string) ([]string, error) {
	endpoints := &corev1.Endpoints{}
	key := client.ObjectKey{Name: serviceName, Namespace: namespace}
	if err := c.Get(context.Background(), key, endpoints); err != nil {
		return nil, err
	}
	var ips []string
	podIP := os.Getenv("POD_IP")
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.IP != podIP {
				ips = append(ips, addr.IP)
			}
		}
	}
	return ips, nil
}

func validateBootstrapToken(c client.Client, token string) bool {
	if !bootstraputil.IsValidBootstrapToken(token) {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	tokenID := parts[0]
	tokenSecret := parts[1]

	secretName := bootstraputil.BootstrapTokenSecretName(tokenID)
	secret := &corev1.Secret{}
	key := client.ObjectKey{Name: secretName, Namespace: "kube-system"}
	if err := c.Get(context.Background(), key, secret); err != nil {
		return false
	}

	if secret.Type != bootstrapapi.SecretTypeBootstrapToken {
		return false
	}

	if string(secret.Data[bootstrapapi.BootstrapTokenSecretKey]) != tokenSecret {
		return false
	}

	if string(secret.Data[bootstrapapi.BootstrapTokenUsageAuthentication]) != "true" {
		return false
	}

	if expiration, ok := secret.Data[bootstrapapi.BootstrapTokenExpirationKey]; ok {
		expTime, err := time.Parse(time.RFC3339, string(expiration))
		if err != nil {
			return false
		}
		if time.Now().After(expTime) {
			return false
		}
	}

	return true
}
