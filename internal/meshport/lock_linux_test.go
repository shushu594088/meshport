//go:build linux

package meshport

import (
	"testing"
	"time"
)

func TestExistingStoreOpensWhileOperationLocked(t *testing.T) {
	a, _, _ := newTestApp(t)
	unlock, e := a.Store.Lock()
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	done := make(chan error, 1)
	go func() {
		s, e := OpenStore(a.Store.dir)
		if e == nil {
			s.Close()
		}
		done <- e
	}()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("ExecStartPre would deadlock behind the caller's operation lock")
	}
}
