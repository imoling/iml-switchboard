package core

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// macOS：用系统自带的 security 命令读写钥匙串。
type securityStore struct{}

func platformSecretStore() secretStore {
	if _, err := exec.LookPath("security"); err != nil {
		return nil
	}
	return securityStore{}
}

func (securityStore) get(service, name string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", name, "-w").Output()
	if err != nil {
		return "", errNoSecret
	}
	return strings.TrimSpace(string(out)), nil
}

func (securityStore) set(service, name, value string) error {
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", name, "-w", value, "-T", "")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New("写入钥匙串失败：" + strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (securityStore) del(service, name string) error {
	_ = exec.Command("security", "delete-generic-password", "-s", service, "-a", name).Run()
	return nil
}
