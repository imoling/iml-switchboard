package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
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

// WinWork 退出后配置里只剩网关地址：真正发请求时才隐藏启动，并发只拉一次，拉不起来算账号问题让请求换候选
func TestEnsureWinWork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".kooky-data", "claude-config")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "settings.json")
	const closed = `{"env":{"ANTHROPIC_BASE_URL":"https://one.example/api"}}`
	const opened = `{"env":{"ANTHROPIC_BASE_URL":"https://one.example/api","ANTHROPIC_AUTH_TOKEN":"tok"}}`
	_ = os.WriteFile(path, []byte(closed), 0o600)

	oldLaunch, oldWait, oldPoll, oldHide := winworkLaunch, winworkWait, winworkPoll, winworkHide
	t.Cleanup(func() {
		winworkLaunch, winworkWait, winworkPoll, winworkHide = oldLaunch, oldWait, oldPoll, oldHide
		winworkStart.failed = time.Time{}
	})
	hidden := atomic.Int32{}
	winworkHide = func() { hidden.Add(1) }
	winworkWait, winworkPoll = 300*time.Millisecond, 5*time.Millisecond
	winworkStart.failed = time.Time{}

	if base, _, err := WinWorkGateway(); base == "" || !errors.Is(err, errWinWorkClosed) {
		t.Fatalf("没开时要给地址和专门的错误：%q %v", base, err)
	}

	// 发请求前才拉起：Account 的 Health 走 APIKey，不会碰这里
	launched := atomic.Int32{}
	winworkLaunch = func() error {
		launched.Add(1)
		go func() { time.Sleep(20 * time.Millisecond); _ = os.WriteFile(path, []byte(opened), 0o600) }()
		return nil
	}
	a := Account{Provider: "winwork", APIKeyGetter: func() (string, error) { _, tok, err := WinWorkGateway(); return tok, err }, BeforeRequest: EnsureWinWork}
	if _, err := a.APIKey(); err == nil {
		t.Fatal("没开时读不到令牌")
	}
	d := NewAPIDriver([]Account{a}, nil)
	_ = d.Health(context.Background())
	if launched.Load() != 0 {
		t.Fatal("看状态不能把应用拉起来")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := EnsureWinWork(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if launched.Load() != 1 || hidden.Load() == 0 {
		t.Fatalf("并发只拉起一次，拉起后要藏起来：%d %d", launched.Load(), hidden.Load())
	}

	// 拉起来了但一直没令牌（没登录）：换候选，一段时间内不反复启动
	_ = os.WriteFile(path, []byte(closed), 0o600)
	launched.Store(0)
	winworkLaunch = func() error { launched.Add(1); return nil }
	err := EnsureWinWork(context.Background())
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !pe.Account || !pe.Retryable || !strings.Contains(pe.Summary, "WinWork 没打开") {
		t.Fatalf("要算账号问题才会换候选：%v", err)
	}
	if err := EnsureWinWork(context.Background()); err == nil || launched.Load() != 1 {
		t.Fatalf("失败后不反复启动：%v %d", err, launched.Load())
	}

	// 用户自己打开后立刻恢复，不用等
	_ = os.WriteFile(path, []byte(opened), 0o600)
	if err := EnsureWinWork(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 没装不算没开，不启动
	_ = os.Remove(path)
	launched.Store(0)
	if err := EnsureWinWork(context.Background()); err != nil || launched.Load() != 0 {
		t.Fatalf("没装时不启动：%v %d", err, launched.Load())
	}
}

// 数据目录不止一种：开发版、环境变量、安装包里改名的都要找到；几个都有时取最近改过的
func TestWinWorkConfigDirVariants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("KOOKY_DATA_DIR", "")
	old := winworkApps
	t.Cleanup(func() { winworkApps = old })
	winworkApps = func() []string { return nil }
	write := func(dir string, at time.Time) {
		p := filepath.Join(dir, "claude-config", "settings.json")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://x/api"}}`), 0o600)
		_ = os.Chtimes(p, at, at)
	}
	if got := WinWorkConfigDir(); got != filepath.Join(home, ".kooky-data", "claude-config") {
		t.Fatalf("都没有时给正式版位置：%s", got)
	}
	now := time.Now()
	write(filepath.Join(home, ".kooky-data-dev"), now)
	if got := WinWorkConfigDir(); got != filepath.Join(home, ".kooky-data-dev", "claude-config") || !WinWorkInstalled() {
		t.Fatalf("开发版要找到：%s", got)
	}
	write(filepath.Join(home, ".kooky-data"), now.Add(-time.Hour))
	if got := WinWorkConfigDir(); got != filepath.Join(home, ".kooky-data-dev", "claude-config") {
		t.Fatalf("取最近改过的：%s", got)
	}
	write(filepath.Join(home, "elsewhere"), now.Add(time.Hour))
	t.Setenv("KOOKY_DATA_DIR", "~/elsewhere")
	if got := WinWorkConfigDir(); got != filepath.Join(home, "elsewhere", "claude-config") {
		t.Fatalf("环境变量指定的要找到：%s", got)
	}
}

// 安装包里声明的目录名：从 asar 包的 package.json 读
func TestWinWorkDataDirFromPackage(t *testing.T) {
	dir := t.TempDir()
	pkg := []byte(`{"name":"kooky","kooky":{"dataDirName":".winwork-custom"}}`)
	header, _ := json.Marshal(map[string]any{"files": map[string]any{"package.json": map[string]any{"offset": "0", "size": len(pkg)}}})
	var b []byte
	le := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	b = append(b, le(4)...)
	b = append(b, le(uint32(len(header)+8))...)
	b = append(b, le(uint32(len(header)+4))...)
	b = append(b, le(uint32(len(header)))...)
	b = append(b, header...)
	b = append(b, pkg...)
	app := filepath.Join(dir, "WinWork.app")
	_ = os.MkdirAll(filepath.Join(app, "Contents", "Resources"), 0o755)
	_ = os.WriteFile(filepath.Join(app, "Contents", "Resources", "app.asar"), b, 0o644)
	old := winworkApps
	t.Cleanup(func() { winworkApps = old })
	winworkApps = func() []string { return []string{app} }
	if got := winworkDataDirName(); got != ".winwork-custom" {
		t.Fatalf("要读到包里的目录名：%q", got)
	}
}
