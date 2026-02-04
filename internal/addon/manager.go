package addon

import (
	"fmt"
	"sync"
)

// AddonRegistry defines the interface for addon registration and retrieval
type AddonRegistry interface {
	// Register registers an addon
	Register(addon Addon)
	// Get returns an addon by name
	Get(name string) Addon
	// List returns all registered addons
	List() []Addon
}

// defaultRegistry is the default implementation of AddonRegistry
type defaultRegistry struct {
	registry map[string]Addon
	lock     sync.RWMutex
}

var (
	// globalRegistry is the global addon registry instance
	globalRegistry = &defaultRegistry{
		registry: make(map[string]Addon),
	}
)

// Register registers an addon to the global registry
func Register(addon Addon) {
	globalRegistry.Register(addon)
}

// Get returns an addon by name from the global registry
func Get(name string) Addon {
	return globalRegistry.Get(name)
}

// List returns all registered addons from the global registry
func List() []Addon {
	return globalRegistry.List()
}

// GetRegistry returns the global registry (useful for testing)
func GetRegistry() AddonRegistry {
	return globalRegistry
}

// NewRegistry creates a new addon registry (useful for testing)
func NewRegistry() AddonRegistry {
	return &defaultRegistry{
		registry: make(map[string]Addon),
	}
}

// Register implements AddonRegistry
func (r *defaultRegistry) Register(addon Addon) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, exists := r.registry[addon.Name()]; exists {
		panic(fmt.Sprintf("Addon %s already registered", addon.Name()))
	}

	r.registry[addon.Name()] = addon
}

// Get implements AddonRegistry
func (r *defaultRegistry) Get(name string) Addon {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.registry[name]
}

// List implements AddonRegistry
func (r *defaultRegistry) List() []Addon {
	r.lock.RLock()
	defer r.lock.RUnlock()

	addons := make([]Addon, 0, len(r.registry))
	for _, addon := range r.registry {
		addons = append(addons, addon)
	}

	return addons
}
