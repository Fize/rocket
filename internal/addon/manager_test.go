package addon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mockAddon implements Addon for testing
type mockAddon struct {
	name string
}

func (m *mockAddon) Name() string {
	return m.name
}

func (m *mockAddon) ManagerController(mgr ctrl.Manager) (AddonController, error) {
	return nil, nil
}

func (m *mockAddon) AgentController(mgr ctrl.Manager) (AddonController, error) {
	return nil, nil
}

func (m *mockAddon) Manifests() []runtime.Object {
	return nil
}

// resetGlobalRegistry resets the global registry for testing
func resetGlobalRegistry() {
	globalRegistry.lock.Lock()
	globalRegistry.registry = make(map[string]Addon)
	globalRegistry.lock.Unlock()
}

func TestRegister(t *testing.T) {
	resetGlobalRegistry()

	addon := &mockAddon{name: "test-addon"}
	Register(addon)

	got := Get("test-addon")
	assert.NotNil(t, got)
	assert.Equal(t, "test-addon", got.Name())
}

func TestRegisterDuplicate(t *testing.T) {
	resetGlobalRegistry()

	addon1 := &mockAddon{name: "duplicate-addon"}
	Register(addon1)

	addon2 := &mockAddon{name: "duplicate-addon"}
	assert.Panics(t, func() {
		Register(addon2)
	}, "Registering duplicate addon should panic")
}

func TestGet(t *testing.T) {
	resetGlobalRegistry()

	// Test Get for non-existent addon
	got := Get("non-existent")
	assert.Nil(t, got)

	// Register and get
	addon := &mockAddon{name: "get-test-addon"}
	Register(addon)

	got = Get("get-test-addon")
	require.NotNil(t, got)
	assert.Equal(t, "get-test-addon", got.Name())
}

func TestList(t *testing.T) {
	resetGlobalRegistry()

	// List empty registry
	addons := List()
	assert.Len(t, addons, 0)

	// Register multiple addons
	addon1 := &mockAddon{name: "list-addon-1"}
	addon2 := &mockAddon{name: "list-addon-2"}
	addon3 := &mockAddon{name: "list-addon-3"}

	Register(addon1)
	Register(addon2)
	Register(addon3)

	addons = List()
	assert.Len(t, addons, 3)

	// Verify all addons are in the list
	names := make(map[string]bool)
	for _, a := range addons {
		names[a.Name()] = true
	}
	assert.True(t, names["list-addon-1"])
	assert.True(t, names["list-addon-2"])
	assert.True(t, names["list-addon-3"])
}

func TestAddonConfig(t *testing.T) {
	config := AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
		Client: nil,
	}

	assert.Equal(t, "test-cluster", config.ClusterName)
	assert.Equal(t, "value1", config.Config["key1"])
	assert.Equal(t, "value2", config.Config["key2"])
}

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	assert.NotNil(t, reg)

	// Test independent registry
	addon1 := &mockAddon{name: "independent-addon"}
	reg.Register(addon1)

	assert.NotNil(t, reg.Get("independent-addon"))
	assert.Len(t, reg.List(), 1)
}

func TestGetRegistry(t *testing.T) {
	reg := GetRegistry()
	assert.NotNil(t, reg)
	assert.Equal(t, globalRegistry, reg)
}

func TestRegistryIsolation(t *testing.T) {
	// Create two independent registries
	reg1 := NewRegistry()
	reg2 := NewRegistry()

	addon1 := &mockAddon{name: "reg1-addon"}
	addon2 := &mockAddon{name: "reg2-addon"}

	reg1.Register(addon1)
	reg2.Register(addon2)

	// Verify isolation
	assert.NotNil(t, reg1.Get("reg1-addon"))
	assert.Nil(t, reg1.Get("reg2-addon"))
	assert.NotNil(t, reg2.Get("reg2-addon"))
	assert.Nil(t, reg2.Get("reg1-addon"))
}

func TestDefaultRegistryMethods(t *testing.T) {
	reg := &defaultRegistry{
		registry: make(map[string]Addon),
	}

	// Test all methods on defaultRegistry directly
	addon := &mockAddon{name: "direct-test"}
	reg.Register(addon)

	assert.NotNil(t, reg.Get("direct-test"))
	assert.Equal(t, "direct-test", reg.Get("direct-test").Name())
	assert.Len(t, reg.List(), 1)
}
