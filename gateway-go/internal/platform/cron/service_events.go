package cron

import "time"

// OnEvent registers an event listener.
// Uses listenersMu (not s.mu) so that emit() can be called while the caller
// holds s.mu without deadlocking.
func (s *Service) OnEvent(listener CronEventListener) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// emit delivers an event to all registered listeners. Safe to call while
// the caller holds s.mu — listeners are guarded by a separate mutex.
func (s *Service) emit(event CronEvent) {
	event.Ts = time.Now().UnixMilli()
	s.listenersMu.RLock()
	listeners := make([]CronEventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.listenersMu.RUnlock()
	for _, l := range listeners {
		l(event)
	}
}
