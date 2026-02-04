package apiserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAPIServerOptions(t *testing.T) {
	opts := NewAPIServerOptions()

	assert.NotNil(t, opts)
	assert.NotNil(t, opts.SecureServing)
	assert.NotNil(t, opts.Authentication)
	assert.NotNil(t, opts.Authorization)
	assert.NotNil(t, opts.Audit)
	assert.NotNil(t, opts.Features)
}

func TestAPIServer_Fields(t *testing.T) {
	apiServer := &APIServer{
		Port: 8443,
	}

	assert.Equal(t, 8443, apiServer.Port)
	assert.Nil(t, apiServer.Client)
	assert.Nil(t, apiServer.TunnelServer)
	assert.Nil(t, apiServer.Scheme)
}

func TestGetOpenAPIDefinitions(t *testing.T) {
	// Test that GetOpenAPIDefinitions returns a valid function
	definitions := GetOpenAPIDefinitions(nil)
	assert.NotNil(t, definitions)
}
