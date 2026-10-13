//go:build !linux

package meshport

// Development on Windows uses a single manager. Production operations use flock on Linux.
func (s *Store) Lock() (func(), error) { s.mu.Lock(); return s.mu.Unlock, nil }
