package relay

import "sync"

type Lifecycle struct {
	mu       sync.RWMutex
	draining bool
	active   int
}

func (l *Lifecycle) BeginSession() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.draining {
		return false
	}
	l.active++
	return true
}

func (l *Lifecycle) EndSession() { l.mu.Lock(); l.active--; l.mu.Unlock() }
func (l *Lifecycle) Drain()      { l.mu.Lock(); l.draining = true; l.mu.Unlock() }
func (l *Lifecycle) Ready() bool { l.mu.RLock(); defer l.mu.RUnlock(); return !l.draining }
func (l *Lifecycle) Live() bool  { return true }
func (l *Lifecycle) Active() int { l.mu.RLock(); defer l.mu.RUnlock(); return l.active }
