package meshport

import "time"

type Settings struct {
	WebPort       int      `json:"web_port"`
	HTTPPort      int      `json:"http_port"`
	SOCKSPort     int      `json:"socks_port"`
	Mode          string   `json:"mode"`
	AllowLAN      bool     `json:"allow_lan"`
	ProxyUser     string   `json:"proxy_user"`
	ProxyPassword string   `json:"proxy_password,omitempty"`
	Autostart     bool     `json:"autostart"`
	TUN           bool     `json:"tun"`
	Bypass        []string `json:"bypass"`
}
type Subscription struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Content   string    `json:"-"`
	UpdatedAt time.Time `json:"updated_at"`
	Kind      string    `json:"kind"`
}
type PendingTUN struct {
	Deadline   time.Time `json:"deadline"`
	Previous   Settings  `json:"previous"`
	WasRunning bool      `json:"was_running"`
}
type State struct {
	Revision      int64                        `json:"revision"`
	Settings      Settings                     `json:"settings"`
	Subscriptions []Subscription               `json:"subscriptions"`
	Active        string                       `json:"active"`
	Selections    map[string]map[string]string `json:"selections"`
	PasswordHash  string                       `json:"password_hash"`
	InitHash      string                       `json:"init_hash"`
	Secret        string                       `json:"secret"`
	Pending       *PendingTUN                  `json:"pending_tun"`
	Notice        string                       `json:"notice"`
}

// Persistence has a separate wire representation: Content is never sent to a browser.
type diskSubscription struct {
	Subscription
	YAML string `json:"yaml"`
}
type diskState struct {
	State
	Subscriptions []diskSubscription `json:"subscriptions"`
}

func defaultState() State {
	return State{Settings: Settings{WebPort: 8888, HTTPPort: 7890, SOCKSPort: 7891, Mode: "rule", Bypass: []string{}}, Subscriptions: []Subscription{}, Selections: map[string]map[string]string{}}
}
func (s State) activeSub() *Subscription {
	for i := range s.Subscriptions {
		if s.Subscriptions[i].ID == s.Active {
			return &s.Subscriptions[i]
		}
	}
	return nil
}
