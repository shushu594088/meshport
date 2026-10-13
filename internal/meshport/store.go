package meshport

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	db  *sql.DB
	dir string
	mu  sync.Mutex
}

func randomToken() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func OpenStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(dir, "meshport.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dir: dir}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=10000; CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS sessions (token TEXT PRIMARY KEY, expires INTEGER NOT NULL);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	// Existing stores are readable without the operation lock. ExecStartPre and
	// the independent recovery process must be able to open them while a caller
	// is holding the lock around systemctl start.
	if _, e = s.Load(); e == nil {
		return s, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		db.Close()
		return nil, e
	}
	unlock, e := s.Lock()
	if e != nil {
		db.Close()
		return nil, e
	}
	defer unlock()
	if _, e = s.Load(); errors.Is(e, sql.ErrNoRows) {
		st := defaultState()
		token := randomToken()
		st.InitHash = digest(token)
		st.Secret = randomToken()
		if e = atomicWrite(filepath.Join(dir, "init-token"), []byte(token+"\n"), 0600); e == nil {
			e = s.Save(st)
		}
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	_ = os.Chmod(filepath.Join(dir, "meshport.db"), 0600)
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Load() (State, error) {
	var raw string
	var d diskState
	e := s.db.QueryRow("SELECT value FROM state WHERE id=1").Scan(&raw)
	if e != nil {
		return State{}, e
	}
	if e = json.Unmarshal([]byte(raw), &d); e != nil {
		return State{}, e
	}
	d.State.Subscriptions = []Subscription{}
	for _, sub := range d.Subscriptions {
		sub.Subscription.Content = sub.YAML
		d.State.Subscriptions = append(d.State.Subscriptions, sub.Subscription)
	}
	return d.State, nil
}
func (s *Store) Save(st State) error {
	st.Revision++
	d := diskState{State: st, Subscriptions: []diskSubscription{}}
	for _, sub := range st.Subscriptions {
		d.Subscriptions = append(d.Subscriptions, diskSubscription{Subscription: sub, YAML: sub.Content})
	}
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO state(id,value) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", string(b))
	return e
}
func (s *Store) Session(token string) bool {
	var expiry int64
	e := s.db.QueryRow("SELECT expires FROM sessions WHERE token=?", digest(token)).Scan(&expiry)
	return e == nil && expiry > time.Now().Unix()
}
func (s *Store) CreateSession() (string, error) {
	token := randomToken()
	_, e := s.db.Exec("INSERT INTO sessions(token,expires) VALUES(?,?)", digest(token), time.Now().Add(12*time.Hour).Unix())
	if e == nil {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE expires < ?", time.Now().Unix())
	}
	return token, e
}
func (s *Store) DeleteSession(token string) {
	_, _ = s.db.Exec("DELETE FROM sessions WHERE token=?", digest(token))
}
func (s *Store) RevokeSessions() error { _, e := s.db.Exec("DELETE FROM sessions"); return e }
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".meshport-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
