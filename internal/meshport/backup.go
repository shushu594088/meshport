package meshport

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

type Backup struct {
	Format    string    `json:"format"`
	CreatedAt time.Time `json:"created_at"`
	Salt      []byte    `json:"salt"`
	Nonce     []byte    `json:"nonce"`
	Data      []byte    `json:"data"`
}

func encryptBackup(st State, password string) (Backup, error) {
	if len(password) < 12 || len(password) > 256 {
		return Backup{}, errors.New("备份密码需为 12–256 字节")
	}
	data, e := json.Marshal(toDisk(st))
	if e != nil {
		return Backup{}, e
	}
	if len(data) > 6<<20 {
		return Backup{}, errors.New("配置备份超过 6 MiB 限制，请减少订阅缓存后再导出")
	}
	salt := make([]byte, 32)
	if _, e = io.ReadFull(rand.Reader, salt); e != nil {
		return Backup{}, e
	}
	block, e := aes.NewCipher(pbkdf2.Key([]byte(password), salt, 600000, 32, sha256.New))
	if e != nil {
		return Backup{}, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return Backup{}, e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return Backup{}, e
	}
	return Backup{Format: "meshport-backup-v1", CreatedAt: time.Now().UTC(), Salt: salt, Nonce: nonce, Data: gcm.Seal(nil, nonce, data, []byte("meshport-backup-v1"))}, nil
}
func decryptBackup(b Backup, password string) (State, error) {
	bad := errors.New("备份格式、密码或内容校验失败")
	if b.Format != "meshport-backup-v1" || len(b.Salt) != 32 || len(b.Nonce) != 12 || len(b.Data) > 6<<20+32 || len(password) < 12 || len(password) > 256 {
		return State{}, bad
	}
	block, e := aes.NewCipher(pbkdf2.Key([]byte(password), b.Salt, 600000, 32, sha256.New))
	if e != nil {
		return State{}, bad
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return State{}, bad
	}
	data, e := gcm.Open(nil, b.Nonce, b.Data, []byte(b.Format))
	if e != nil {
		return State{}, bad
	}
	var disk diskState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&disk) != nil {
		return State{}, bad
	}
	return fromDisk(disk), nil
}
func validateRestore(next *State, old State, port int) error {
	if len(next.Subscriptions) > 64 {
		return errors.New("备份中订阅数量超过 64 个")
	}
	next.PasswordHash = old.PasswordHash
	next.Secret = old.Secret
	next.InitHash = old.InitHash
	next.Revision = old.Revision
	next.Pending = nil
	next.Notice = ""
	next.Settings.WebPort = old.Settings.WebPort
	next.Settings.TUN = false
	if next.Selections == nil {
		next.Selections = map[string]map[string]string{}
	}
	if e := validateSettings(next.Settings, port); e != nil {
		return e
	}
	ids := map[string]bool{}
	validID := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for i := range next.Subscriptions {
		s := &next.Subscriptions[i]
		if !validID.MatchString(s.ID) || ids[s.ID] || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 80 {
			return errors.New("备份中订阅名称或标识无效")
		}
		ids[s.ID] = true
		_, kind, e := parseSubscription(s.Content)
		if e != nil {
			return e
		}
		s.Kind = kind
		if e = validateDNS(s.DNS); e != nil {
			return e
		}
	}
	if next.Active != "" && !ids[next.Active] {
		return errors.New("备份的当前订阅不存在")
	}
	return nil
}
