package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// WinWork（讯飞）是自家产品，它的网关是普通的 OpenAI 兼容接口，令牌在它内嵌 Claude Code 的配置里。
// 这不属于第三方订阅红线：令牌每次请求现读，不落盘、不转发给别处，只用于它自己的网关。

// WinWorkConfigDir 是 WinWork 内嵌 Claude Code 的配置目录。数据目录不固定（它的 tenant/kooky-data-dir.js）：
// 环境变量 KOOKY_DATA_DIR 整个换掉；安装包 package.json 的 kooky.dataDirName 可改名；开发版是 ~/.kooky-data-dev，正式版 ~/.kooky-data。
// 按这个思路找，几个都有配置时取最近改过的；都没有时给正式版的位置，报错文案按它说。
func WinWorkConfigDir() string {
	home, _ := os.UserHomeDir()
	var dirs []string
	if d := strings.TrimSpace(os.Getenv("KOOKY_DATA_DIR")); d != "" {
		if strings.HasPrefix(d, "~/") {
			d = filepath.Join(home, d[2:])
		} else if !filepath.IsAbs(d) {
			d = filepath.Join(home, d)
		}
		dirs = append(dirs, d)
	}
	if name := winworkDataDirName(); name != "" {
		if filepath.IsAbs(name) {
			dirs = append(dirs, name)
		} else {
			dirs = append(dirs, filepath.Join(home, name))
		}
	}
	found, _ := filepath.Glob(filepath.Join(home, ".kooky-data*"))
	dirs = append(dirs, found...)
	best := filepath.Join(home, ".kooky-data", "claude-config")
	var bestAt time.Time
	for _, d := range dirs {
		dir := filepath.Join(d, "claude-config")
		if st, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil && st.ModTime().After(bestAt) {
			best, bestAt = dir, st.ModTime()
		}
	}
	return best
}

// winworkApps 是 WinWork 可能装在的位置，测试里会换掉。
var winworkApps = func() []string {
	home, _ := os.UserHomeDir()
	return []string{"/Applications/WinWork.app", filepath.Join(home, "Applications", "WinWork.app")}
}

// winworkDataDirName 读已安装 WinWork 包里声明的数据目录名（package.json 的 kooky.dataDirName），没有返回空串。
// 包是 Electron 的 asar：8 字节头之后是 4 字节长度，再往后是一段 JSON 目录，文件内容紧跟其后。
func winworkDataDirName() string {
	for _, app := range winworkApps() {
		b, err := readAsarFile(filepath.Join(app, "Contents", "Resources", "app.asar"), "package.json")
		if err != nil {
			continue
		}
		var pkg struct {
			Kooky struct {
				DataDirName      string `json:"dataDirName"`
				KookyDataDirName string `json:"kookyDataDirName"`
			} `json:"kooky"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			if n := strings.TrimSpace(pkg.Kooky.DataDirName); n != "" {
				return n
			}
			return strings.TrimSpace(pkg.Kooky.KookyDataDirName)
		}
	}
	return ""
}

// readAsarFile 从 asar 包的根目录读一个小文件（只读目录和这一个文件，不解整个包）。
func readAsarFile(asar, name string) ([]byte, error) {
	f, err := os.Open(asar)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 16)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(head[4:8])
	jsonLen := binary.LittleEndian.Uint32(head[12:16])
	if jsonLen == 0 || jsonLen > 64<<20 {
		return nil, errors.New("不是 asar 包")
	}
	raw := make([]byte, jsonLen)
	if _, err := io.ReadFull(f, raw); err != nil {
		return nil, err
	}
	var dir struct {
		Files map[string]struct {
			Offset string `json:"offset"`
			Size   int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &dir); err != nil {
		return nil, err
	}
	e, ok := dir.Files[name]
	if !ok || e.Size <= 0 || e.Size > 1<<20 {
		return nil, errors.New("包里没有 " + name)
	}
	off, err := strconv.ParseInt(e.Offset, 10, 64)
	if err != nil {
		return nil, err
	}
	out := make([]byte, e.Size)
	_, err = f.ReadAt(out, 8+int64(size)+off)
	return out, err
}

// WinWorkInstalled 判断本机装没装 WinWork 且登录过（有它的 settings.json）。
func WinWorkInstalled() bool {
	_, err := os.Stat(filepath.Join(WinWorkConfigDir(), "settings.json"))
	return err == nil
}

// errWinWorkClosed：WinWork 退出和登出时都会把令牌从配置里删掉，只留网关地址。有地址没令牌就是它没开。
var errWinWorkClosed = &core.ErrKeyNotHere{Reason: "WinWork 没打开，有请求时会自动启动"}

// WinWorkGateway 读出网关地址（已拼到 /v1）和令牌。WinWork 每次启动会刷新令牌，所以不缓存。
// 它没开时返回地址和 errWinWorkClosed。
func WinWorkGateway() (baseURL, token string, err error) {
	b, err := os.ReadFile(filepath.Join(WinWorkConfigDir(), "settings.json"))
	if err != nil {
		return "", "", errors.New("本机没有安装 WinWork，或还没登录过")
	}
	var s struct {
		Env struct {
			BaseURL string `json:"ANTHROPIC_BASE_URL"`
			Token   string `json:"ANTHROPIC_AUTH_TOKEN"`
		} `json:"env"`
	}
	if json.Unmarshal(b, &s) != nil || s.Env.BaseURL == "" {
		return "", "", errors.New("WinWork 的配置里没有网关信息：打开 WinWork 登录一次")
	}
	base := strings.TrimRight(s.Env.BaseURL, "/") + "/v1"
	if s.Env.Token == "" {
		return base, "", errWinWorkClosed
	}
	return base, s.Env.Token, nil
}

// 拉起 WinWork 的参数，测试里会换掉。
var (
	winworkLaunch = func() error { return exec.Command("open", "-g", "-j", "-a", "WinWork").Run() } // 不抢焦点、不显示窗口
	// open -j 拦不住它自己弹窗，等它起来后再藏；用系统的 NSRunningApplication，不需要额外授权
	winworkHide = func() {
		_ = exec.Command("osascript", "-l", "JavaScript", "-e", `ObjC.import('AppKit');$.NSRunningApplication.runningApplicationsWithBundleIdentifier('com.agentos.desktop').js.forEach(function(a){a.hide})`).Run()
	}
	winworkWait  = 30 * time.Second // 等它把令牌写回来的上限
	winworkPoll  = 300 * time.Millisecond
	winworkRetry = 2 * time.Minute // 拉起失败后这么久内不再试：没登录时反复启动没有用
)

var winworkStart struct {
	mu     sync.Mutex // 并发请求排队，只拉起一次
	failed time.Time
}

// EnsureWinWork 在真正发请求之前调用：WinWork 没开就隐藏启动，等令牌写回来。令牌仍是现读，这里不碰它的内容。
// 健康检查和看账号状态不走这里，否则看一眼状态就会把应用拉起来。
// 失败算账号问题：这个账号先歇 30 秒，请求换下一个候选。
func EnsureWinWork(ctx context.Context) error {
	open := func() bool { _, _, err := WinWorkGateway(); return !errors.Is(err, errWinWorkClosed) }
	if open() {
		return nil // 包括没装的情况：那是另一种错，读令牌时会报
	}
	winworkStart.mu.Lock()
	defer winworkStart.mu.Unlock()
	if open() {
		return nil // 排队时前一个请求已经拉起来了
	}
	unavailable := func(summary, raw string) error {
		return &core.ErrProvider{Summary: summary, Raw: raw, Retryable: true, Account: true, Cooldown: 30 * time.Second}
	}
	if time.Since(winworkStart.failed) < winworkRetry {
		return unavailable("WinWork 没打开：打开后重试", "刚才自动启动没成功，暂不重试")
	}
	if err := winworkLaunch(); err != nil {
		winworkStart.failed = time.Now()
		return unavailable("WinWork 没打开：自动启动失败，手动打开后重试", err.Error())
	}
	deadline := time.After(winworkWait)
	tick := time.NewTicker(winworkPoll)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if open() {
				winworkStart.failed = time.Time{}
				winworkHide()
				time.AfterFunc(2*time.Second, winworkHide) // 窗口有时比令牌晚出来
				return nil
			}
		case <-deadline:
			winworkStart.failed = time.Now()
			return unavailable("WinWork 没打开：启动后没拿到令牌，打开它确认已登录", "等了 "+winworkWait.String())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func winworkPreset() Preset {
	models := map[string]ModelPrice{}
	for _, w := range core.WinWorkModels {
		models[w.ID] = ModelPrice{}
	}
	// BaseURL 只是默认值，装配账号时以 WinWork 配置里的为准
	return Preset{ID: "winwork", Name: "WinWork", BaseURL: "https://one.iflytek.com/api/llm/console/chat/v1", Currency: "CNY", ProbeModel: "winwork-ds-v4.1-flash", Models: models}
}

func init() { Presets = append(Presets, winworkPreset()) }
