package job

import (
	"errors"
	"sync"
)

// Registry is the in-process dispatch table for durable job types. The job
// type is persisted, while the handler remains an application concern.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

func (registry *Registry) Register(jobType string, handler Handler) error {
	if jobType == "" || handler == nil {
		return errors.New("job type and handler are required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.handlers[jobType]; exists {
		return errors.New("job handler already registered")
	}
	registry.handlers[jobType] = handler
	return nil
}

func (registry *Registry) Handler(jobType string) (Handler, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	handler, ok := registry.handlers[jobType]
	return handler, ok
}

var _ HandlerRegistry = (*Registry)(nil)
