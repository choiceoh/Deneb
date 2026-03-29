package cron

import "time"

// OnEvent registers an event listener.
func (s *Service) OnEvent(listener CronEventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *Service) emit(event CronEvent) {
	event.Ts = time.Now().UnixMilli()
	s.mu.Lock()
	listeners := make([]CronEventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		l(event)
	}
}
