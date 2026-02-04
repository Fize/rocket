package apiserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/hex-techs/rocket/internal/manager/apiserver/handler"
	clusterregistry "github.com/hex-techs/rocket/internal/manager/apiserver/registry/cluster"
	"github.com/rancher/remotedialer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1"
)

type APIServer struct {
	Client       client.Client
	TunnelServer *remotedialer.Server
	Port         int
	Scheme       *runtime.Scheme
}

type APIServerOptions struct {
	SecureServing  *options.SecureServingOptionsWithLoopback
	Authentication *options.DelegatingAuthenticationOptions
	Authorization  *options.DelegatingAuthorizationOptions
	Audit          *options.AuditOptions
	Features       *options.FeatureOptions
}

func NewAPIServerOptions() *APIServerOptions {
	return &APIServerOptions{
		SecureServing:  options.NewSecureServingOptions().WithLoopback(),
		Authentication: options.NewDelegatingAuthenticationOptions(),
		Authorization:  options.NewDelegatingAuthorizationOptions(),
		Audit:          options.NewAuditOptions(),
		Features:       options.NewFeatureOptions(),
	}
}

func (s *APIServer) Start(ctx context.Context) error {
	fmt.Println("DEBUG: Starting APIServer with custom configuration to disable OpenAPI")
	o := NewAPIServerOptions()
	o.SecureServing.BindPort = s.Port
	o.SecureServing.ServerCert.CertDirectory = "/tmp" // Ensure we can write self-signed certs

	codecs := serializer.NewCodecFactory(s.Scheme)

	if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(codecs)

	// Configure OpenAPI with dummy definitions to prevent crash due to missing model definitions
	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(GetOpenAPIDefinitions, openapi.NewDefinitionNamer(s.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "Rocket"
	serverConfig.OpenAPIConfig.Info.Version = "0.1"

	// Skip installation of the endpoint itself, but we still provided the config to satisfy the builder dependencies
	serverConfig.Config.SkipOpenAPIInstallation = true

	// Provide a minimal OpenAPI V3 config (cannot be nil)
	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(GetOpenAPIDefinitions, openapi.NewDefinitionNamer(s.Scheme))
	serverConfig.OpenAPIV3Config.Info.Title = "Rocket"
	serverConfig.OpenAPIV3Config.Info.Version = "0.1"

	if err := o.SecureServing.ApplyTo(&serverConfig.SecureServing, &serverConfig.LoopbackClientConfig); err != nil {
		return err
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		if err := o.Authentication.ApplyTo(&serverConfig.Authentication, serverConfig.SecureServing, nil); err != nil {
			return err
		}
		if err := o.Authorization.ApplyTo(&serverConfig.Authorization); err != nil {
			return err
		}
	}

	serverConfig.Config.BuildHandlerChainFunc = func(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
		handler := genericapiserver.DefaultBuildHandlerChain(apiHandler, c)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/connect" || strings.HasPrefix(r.URL.Path, "/proxy/") || r.URL.Path == "/internal/check" {
				apiHandler.ServeHTTP(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}

	completedConfig := serverConfig.Complete()
	// Check if we can intercept the config here? No, CompletedConfig fields are private.

	server, err := completedConfig.New("rocket-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return err
	}

	if err := s.installAPIResources(server, s.Scheme, codecs); err != nil {
		return err
	}

	handler.InstallHandler(server.Handler.NonGoRestfulMux, s.Client, s.TunnelServer)

	return server.PrepareRun().Run(ctx.Done())
}

func (s *APIServer) installAPIResources(server *genericapiserver.GenericAPIServer, scheme *runtime.Scheme, codecs serializer.CodecFactory) error {
	v1alpha1storage := map[string]rest.Storage{}
	v1alpha1storage["clusters"] = clusterregistry.NewREST(s.Client)
	v1alpha1storage["clusters/proxy"] = clusterregistry.NewProxyREST(s.Client, s.TunnelServer)

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(clusterv1alpha1.GroupVersion.Group, scheme, metav1.ParameterCodec, codecs)
	apiGroupInfo.VersionedResourcesStorageMap[clusterv1alpha1.GroupVersion.Version] = v1alpha1storage
	// Set the priority ordered version list (required by InstallAPIGroup)
	apiGroupInfo.PrioritizedVersions = []schema.GroupVersion{clusterv1alpha1.GroupVersion}

	return server.InstallAPIGroup(&apiGroupInfo)
}
