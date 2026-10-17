package runtime

import (
	"sync"
)

type Manager struct {
	local         Runtime
	mu            sync.RWMutex
	localOverride Runtime
}

func NewManager(deps LocalDeps) *Manager { return &Manager{local: NewLocal(deps)} }
func (m *Manager) SetLocalRuntimeOverride(rt Runtime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localOverride = rt
}

func (m *Manager) Local() Runtime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.localOverride != nil {
		return m.localOverride
	}
	return m.local
}

var (
	managerMu sync.RWMutex
	manager   *Manager
)

func SetManager(m *Manager) { managerMu.Lock(); defer managerMu.Unlock(); manager = m }
func GetManager() *Manager  { managerMu.RLock(); defer managerMu.RUnlock(); return manager }
