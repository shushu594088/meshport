package meshport

import (
	"context"
	"fmt"
	"meshport/internal/ui"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Opt-in local browser fixture: no real proxy, privileges or public traffic.
type browserKernel struct {
	mu       sync.Mutex
	selected string
}

func (*browserKernel) Validate([]byte) error      { return nil }
func (*browserKernel) Apply([]byte, string) error { return nil }
func (*browserKernel) Healthy(string) bool        { return true }
func (k *browserKernel) Proxies(string) (map[string]Proxy, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	names := []string{"浏览器直连节点", "浏览器备用节点"}
	proxies := map[string]Proxy{"PROXY": {Name: "PROXY", Type: "Selector", All: names, Now: k.selected}}
	for _, name := range names {
		proxies[name] = Proxy{Name: name, Type: "Direct"}
	}
	return proxies, nil
}
func (k *browserKernel) Select(_, name, _ string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.selected = name
	return nil
}
func (*browserKernel) Delay(context.Context, string, string) (int, error) { return 24, nil }

type browserService struct {
	mu      sync.Mutex
	running bool
}

func (s *browserService) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ServiceStatus{Running: s.running, Available: true, Driver: "browser fixture"}
}
func (s *browserService) Action(action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = action == "start" || action == "restart"
	return nil
}
func TestBrowserFixtureServer(t *testing.T) {
	if os.Getenv("MESHPORT_BROWSER_FIXTURE") != "1" {
		t.Skip("local browser fixture is opt-in")
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, _ := store.Load()
	hash, _ := bcrypt.GenerateFromPassword([]byte("meshport-test-password-2026"), bcrypt.DefaultCost)
	state.PasswordHash = string(hash)
	state.InitHash = ""
	if err = store.Save(state); err != nil {
		t.Fatal(err)
	}
	a := &App{Store: store, Kernel: &browserKernel{selected: "浏览器直连节点"}, Service: &browserService{}, ControllerPort: 9090}
	h := &HTTP{App: a, Host: "127.0.0.1", Assets: ui.Handler(), Version: "0.1.1"}
	if err = h.Listen(18888); err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown(context.Background())
	fmt.Println("Browser fixture ready at http://127.0.0.1:18888")
	<-time.After(10 * time.Minute)
}
