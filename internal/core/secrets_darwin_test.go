package core

import (
	"errors"
	"strings"
	"testing"
)

// 旧条目第一次读出来后换成信任 security 命令的写法，之后不再重建；新写入的条目直接是新写法
func TestKeychainRetrust(t *testing.T) {
	var calls []string
	stored := map[string]string{"agnes": "old-key"}
	failAdd := 0
	old := runSecurity
	t.Cleanup(func() { runSecurity = old })
	runSecurity = func(bin string, args ...string) ([]byte, error) {
		calls = append(calls, args[0]+" "+strings.Join(args[len(args)-2:], " "))
		name := ""
		for i, a := range args {
			if a == "-a" {
				name = args[i+1]
			}
		}
		switch args[0] {
		case "find-generic-password":
			v, ok := stored[name]
			if !ok {
				return nil, errors.New("not found")
			}
			return []byte(v + "\n"), nil
		case "delete-generic-password":
			delete(stored, name)
		case "add-generic-password":
			if failAdd > 0 {
				failAdd--
				return nil, errors.New("busy")
			}
			for i, a := range args {
				if a == "-w" {
					stored[name] = args[i+1]
				}
			}
		}
		return nil, nil
	}
	s := &securityStore{bin: "/usr/bin/security", checked: map[string]bool{}}

	failAdd = 1 // 重建时第一次写失败也不能丢密钥
	if v, err := s.get("svc", "agnes"); err != nil || v != "old-key" {
		t.Fatalf("%q %v", v, err)
	}
	if stored["agnes"] != "old-key" {
		t.Fatalf("重建后密钥不能丢：%v", stored)
	}
	last := calls[len(calls)-1]
	if !strings.HasPrefix(last, "add-generic-password") || !strings.HasSuffix(last, "-T /usr/bin/security") {
		t.Fatalf("要信任 security 命令：%v", calls)
	}
	n := len(calls)
	_, _ = s.get("svc", "agnes")
	if len(calls) != n+1 {
		t.Fatalf("同一条目只重建一次：%v", calls[n:])
	}

	_ = s.set("svc", "custom-1", "k")
	n = len(calls)
	_, _ = s.get("svc", "custom-1")
	if !strings.HasSuffix(calls[n-1], "-T /usr/bin/security") || len(calls) != n+1 {
		t.Fatalf("新写入的不用再重建：%v", calls)
	}
	if _, err := s.get("svc", "nope"); err == nil {
		t.Fatal("没有的条目要报错")
	}
}
