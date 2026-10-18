package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// KeychainSecrets 把按量付费密钥存进系统自带的凭据库：macOS 钥匙串、Windows 凭据管理器、Linux Secret Service。
// 只存我们自己创建的条目（服务名固定，账户名是账号 id），从不碰别的应用（包括 Claude、Codex）的条目。
// 系统凭据库不可用时退回数据目录下的文件（权限 0600），Plain() 为真，doctor 会提醒。
type KeychainSecrets struct {
	service  string
	store    secretStore // nil 表示用文件
	fallback string
	mu       sync.Mutex
}

// secretStore 是各平台的系统凭据库。
type secretStore interface {
	get(service, name string) (string, error)
	set(service, name, value string) error
	del(service, name string) error
}

var errNoSecret = errors.New("没有找到密钥")

// NewSecrets 创建密钥存取。设了 IMLS_SECRETS_FILE（测试用）时一律走文件。
func NewSecrets(dataDir string) *KeychainSecrets {
	s := &KeychainSecrets{service: "iML Switchboard"}
	if os.Getenv("IMLS_SECRETS_FILE") == "" {
		s.store = platformSecretStore()
	}
	if s.store == nil {
		s.fallback = filepath.Join(dataDir, "secrets.local")
	}
	return s
}

// Plain 表示密钥是以文件形式存的（系统凭据库不可用）。
func (s *KeychainSecrets) Plain() bool { return s.store == nil }

// Get 读取密钥。
func (s *KeychainSecrets) Get(name string) (string, error) {
	if s.store == nil {
		return s.fileGet(name)
	}
	return s.store.get(s.service, name)
}

// Set 写入密钥（覆盖）。
func (s *KeychainSecrets) Set(name, value string) error {
	if s.store == nil {
		return s.fileSet(name, value)
	}
	return s.store.set(s.service, name, value)
}

// Delete 删除密钥。
func (s *KeychainSecrets) Delete(name string) error {
	if s.store == nil {
		return s.fileSet(name, "")
	}
	return s.store.del(s.service, name)
}

func (s *KeychainSecrets) fileGet(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := os.ReadFile(s.fallback)
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && k == name && v != "" {
			return v, nil
		}
	}
	return "", errNoSecret
}

func (s *KeychainSecrets) fileSet(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := os.ReadFile(s.fallback)
	var keep []string
	for _, line := range strings.Split(string(b), "\n") {
		if k, _, ok := strings.Cut(line, "="); ok && k != name {
			keep = append(keep, line)
		}
	}
	if value != "" {
		keep = append(keep, name+"="+value)
	}
	return os.WriteFile(s.fallback, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
}
