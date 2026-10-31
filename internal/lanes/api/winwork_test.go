package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWinWorkGateway(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if WinWorkInstalled() {
		t.Fatal("空目录不该算已安装")
	}
	if _, _, err := WinWorkGateway(); err == nil {
		t.Fatal("没装时应报错")
	}
	dir := filepath.Join(home, ".kooky-data", "claude-config")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model":"x","env":{"ANTHROPIC_BASE_URL":"https://one.example/api/llm/console/chat/","ANTHROPIC_AUTH_TOKEN":"tok-123"}}`), 0o600)
	base, tok, err := WinWorkGateway()
	if err != nil || base != "https://one.example/api/llm/console/chat/v1" || tok != "tok-123" || !WinWorkInstalled() {
		t.Fatalf("%q %q %v", base, tok, err)
	}
	p, ok := PresetByID("winwork")
	if !ok || p.ProbeModel == "" || len(p.Models) < 8 {
		t.Fatalf("预设不全：%+v", p)
	}
}
