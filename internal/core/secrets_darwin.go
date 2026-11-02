package core

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"sync"
)

// macOS：用系统自带的 security 命令读写钥匙串。
// 条目只信任 security 命令自己（-T），读的时候才不会每次弹「security 想要使用钥匙串中的机密信息」。
// 代价：用这个账户跑的程序只要知道条目名，也能用 security 命令读到，和用户在弹框里点「始终允许」一样。
type securityStore struct {
	bin string // security 命令的绝对路径，写进条目的信任列表

	mu      sync.Mutex
	checked map[string]bool // 本次运行里已经确认是新写法的条目
}

// runSecurity 跑一次 security 命令，测试里会换掉。
var runSecurity = func(bin string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return out, errors.New(strings.TrimSpace(stderr.String()))
	}
	return out, err
}

func platformSecretStore() secretStore {
	bin, err := exec.LookPath("security")
	if err != nil {
		return nil
	}
	return &securityStore{bin: bin, checked: map[string]bool{}}
}

func (s *securityStore) get(service, name string) (string, error) {
	out, err := runSecurity(s.bin, "find-generic-password", "-s", service, "-a", name, "-w")
	if err != nil {
		return "", errNoSecret
	}
	value := strings.TrimSpace(string(out))
	s.mu.Lock()
	done := s.checked[name]
	s.checked[name] = true
	s.mu.Unlock()
	if !done {
		s.retrust(service, name, value)
	}
	return value, nil
}

// retrust 把旧写法的条目换成新写法。旧版本写的是 -T ""（谁都不信任），每次读都弹框；
// 能读出来说明用户允许过，删掉按新写法重建一次，以后就不弹了。删除实测不需要授权、不弹框。
// 已经是新写法的条目重建一次也无妨；每次运行每个条目只做一次。
func (s *securityStore) retrust(service, name, value string) {
	_, _ = runSecurity(s.bin, "delete-generic-password", "-s", service, "-a", name)
	for i := 0; i < 3; i++ { // 删了再写失败，密钥就丢了：多试两次
		if s.add(service, name, value) == nil {
			return
		}
	}
}

func (s *securityStore) add(service, name, value string) error {
	_, err := runSecurity(s.bin, "add-generic-password", "-U", "-s", service, "-a", name, "-w", value, "-T", s.bin)
	return err
}

func (s *securityStore) set(service, name, value string) error {
	if err := s.add(service, name, value); err != nil {
		return errors.New("写入钥匙串失败：" + err.Error())
	}
	s.mu.Lock()
	s.checked[name] = true
	s.mu.Unlock()
	return nil
}

func (s *securityStore) del(service, name string) error {
	_, _ = runSecurity(s.bin, "delete-generic-password", "-s", service, "-a", name)
	return nil
}
