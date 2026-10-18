package core

import (
	"errors"
	"os/exec"
	"strings"
)

// Linux：桌面环境的 Secret Service（GNOME 钥匙圈、KWallet），经 libsecret 自带的 secret-tool 命令读写。
// 没装 secret-tool（比如纯命令行服务器）时返回 nil，退回文件。
type secretToolStore struct{}

func platformSecretStore() secretStore {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil
	}
	return secretToolStore{}
}

func (secretToolStore) get(service, name string) (string, error) {
	out, err := exec.Command("secret-tool", "lookup", "service", service, "account", name).Output()
	if err != nil || len(out) == 0 {
		return "", errNoSecret
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (secretToolStore) set(service, name, value string) error {
	cmd := exec.Command("secret-tool", "store", "--label="+service+" "+name, "service", service, "account", name)
	cmd.Stdin = strings.NewReader(value) // 从标准输入传，密钥不出现在进程列表里
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("写入系统钥匙圈失败：" + strings.TrimSpace(string(out)))
	}
	return nil
}

func (secretToolStore) del(service, name string) error {
	_ = exec.Command("secret-tool", "clear", "service", service, "account", name).Run()
	return nil
}
