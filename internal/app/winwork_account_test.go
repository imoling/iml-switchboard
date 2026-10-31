package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 装了 WinWork 就自动多一个不计费的账号；重复发现不会加第二个
func TestEnsureWinWorkAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("IMLS_DATA_DIR", filepath.Join(home, "data"))
	cs, err := config.Open()
	if err != nil {
		t.Fatal(err)
	}
	if ensureWinWorkAccount(cs) {
		t.Fatal("没装 WinWork 不该加账号")
	}
	dir := filepath.Join(home, ".kooky-data", "claude-config")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://one.example/chat","ANTHROPIC_AUTH_TOKEN":"tok"}}`), 0o600)
	if !ensureWinWorkAccount(cs) || ensureWinWorkAccount(cs) {
		t.Fatal("第一次应加，第二次不该再加")
	}
	n := 0
	for _, a := range cs.Get().API {
		if a.Provider == "winwork" {
			n++
			if !a.Enabled || a.ID != "winwork" || len(a.Models["high"]) == 0 {
				t.Fatalf("账号内容不对：%+v", a)
			}
		}
	}
	if n != 1 {
		t.Fatalf("应只有一个 WinWork 账号：%d", n)
	}
	if tok, err := (secretsWithWinWork{nil}).Get("winwork"); err != nil || tok != "tok" {
		t.Fatalf("令牌应从 WinWork 配置现读：%q %v", tok, err)
	}
}
