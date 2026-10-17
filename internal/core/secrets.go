package core

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// KeychainSecrets 用 macOS 的 security 命令存按量付费密钥。服务名固定，账户名是账号 id。
// 这里只存我们自己创建的条目，从不碰别的应用（包括 Claude、Codex）的钥匙串项。
type KeychainSecrets struct {
	service string
	fallback string // 非 macOS 或 security 不可用时的文件回退（仅测试）
	mu sync.Mutex
}

// NewSecrets 创建密钥存取。
func NewSecrets(dataDir string) *KeychainSecrets {
	s := &KeychainSecrets{service: "iML Switchboard"}
	if _, err := exec.LookPath("security"); err != nil || os.Getenv("IMLS_SECRETS_FILE") != "" {
		s.fallback = filepath.Join(dataDir, "secrets.local")
	}
	return s
}

// Get 读取密钥。
func (s *KeychainSecrets) Get(name string) (string, error) {
	if s.fallback != "" {
		return s.fileGet(name)
	}
	out, err := exec.Command("security", "find-generic-password", "-s", s.service, "-a", name, "-w").Output()
	if err != nil {
		return "", errors.New("没有找到密钥")
	}
	return strings.TrimSpace(string(out)), nil
}

// Set 写入密钥（覆盖）。
func (s *KeychainSecrets) Set(name, value string) error {
	if s.fallback != "" {
		return s.fileSet(name, value)
	}
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", s.service, "-a", name, "-w", value, "-T", "")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New("写入钥匙串失败：" + strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Delete 删除密钥。
func (s *KeychainSecrets) Delete(name string) error {
	if s.fallback != "" {
		return s.fileSet(name, "")
	}
	_ = exec.Command("security", "delete-generic-password", "-s", s.service, "-a", name).Run()
	return nil
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
	return "", errors.New("没有找到密钥")
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
