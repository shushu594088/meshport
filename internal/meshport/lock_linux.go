//go:build linux

package meshport

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func (s *Store) Lock() (func(), error) {
	s.mu.Lock()
	f, e := os.OpenFile(filepath.Join(s.dir, "operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		s.mu.Unlock()
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		f.Close()
		s.mu.Unlock()
		return nil, e
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close(); s.mu.Unlock() }, nil
}
