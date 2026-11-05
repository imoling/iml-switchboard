// Package laya 管本机的 Laya 决策模型（Convai Innovations，Apache-2.0）：装运行环境、下载权重、起停 laya-serve。
// laya-serve 说的是 Jev 的 /v1/systemone 协议，智能分派直接用 Jev 的客户端连它。
// 一切都放在数据目录的 laya/ 下：venv/ 是 Python 运行环境，hf/ 是 Hugging Face 缓存，pid、key、日志各一个文件。
package laya

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Version     = "0.4.22"                 // 装哪一版：跟着实测过的走，升级要重新测
	Repo        = "convaiinnovations/laya" // 权重在这个仓库的 multilingual/ 下
	Checkpoint  = "multilingual"           // 中文任务用多语言版（mmBERT-base，322M）
	ModelBytes  = 678_000_000              // 多语言版权重大小，算下载进度用
	idleUnload  = 600                      // 空闲 10 分钟卸载权重、放回内存，下次请求自动加载
	startupWait = 90 * time.Second
)

// Port 是本机服务的端口，只监听 127.0.0.1；测试里换成空闲端口，免得探到真在跑的 Laya
var Port = 18120

// Status 是 Laya 在本机的状态。
type Status struct {
	Installed  bool    `json:"installed"`  // 运行环境装好了
	Downloaded bool    `json:"downloaded"` // 权重下好了
	Running    bool    `json:"running"`
	Busy       string  `json:"busy,omitempty"`     // installing downloading starting
	Progress   float64 `json:"progress,omitempty"` // 下载进度 0～1
	Device     string  `json:"device,omitempty"`   // mps cpu
	Error      string  `json:"error,omitempty"`
	DiskMB     int     `json:"disk_mb"`
}

// Manager 管一个 Laya。
type Manager struct {
	Dir string
	// Env 是给子进程的环境：去掉下载会变慢的代理之类，由调用方决定
	Env func() []string

	mu   sync.Mutex
	busy string
	err  string
}

func (m *Manager) venvBin(name string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(m.Dir, "venv", "Scripts", name+".exe")
	}
	return filepath.Join(m.Dir, "venv", "bin", name)
}
func (m *Manager) hfHome() string  { return filepath.Join(m.Dir, "hf") }
func (m *Manager) pidFile() string { return filepath.Join(m.Dir, "serve.pid") }
func (m *Manager) logFile() string { return filepath.Join(m.Dir, "serve.log") }

// Endpoint 是本机服务的 Jev 兼容接口。
func Endpoint() string { return fmt.Sprintf("http://127.0.0.1:%d/v1/systemone", Port) }

// Key 是本机服务的密钥：第一次用时随机生成，存在 laya/key 里，只有 Switchboard 和它自己知道。
func (m *Manager) Key() (string, error) {
	p := filepath.Join(m.Dir, "key")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	k := hex.EncodeToString(buf)
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return "", err
	}
	return k, os.WriteFile(p, []byte(k), 0o600)
}

func (m *Manager) installed() bool {
	_, err := os.Stat(m.venvBin("laya-serve"))
	return err == nil
}

// weights 是已下载的权重文件，找到 model.safetensors 才算下好。
func (m *Manager) weights() (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(m.hfHome(), "hub", "models--convaiinnovations--laya", "snapshots", "*", Checkpoint, "model.safetensors"))
	for _, p := range matches {
		if st, err := os.Stat(p); err == nil && st.Size() > ModelBytes/2 {
			return p, true
		}
	}
	return "", false
}

func dirBytes(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// Status 读一次状态；在跑的时候问一下 /health 拿设备。
func (m *Manager) Status() Status {
	m.mu.Lock()
	busy, errText := m.busy, m.err
	m.mu.Unlock()
	st := Status{Installed: m.installed(), Busy: busy, Error: errText, DiskMB: int(dirBytes(m.Dir) / 1e6)}
	_, st.Downloaded = m.weights()
	if busy == "downloading" {
		st.Progress = min(float64(dirBytes(m.hfHome()))/ModelBytes, 0.99)
	}
	if dev, ok := m.health(); ok {
		st.Running, st.Device = true, dev
	}
	return st
}

// Running 只问一下 /health：每次判官调用前用，比 Status 便宜（不扫目录）。
func (m *Manager) Running() bool { _, ok := m.health(); return ok }

func (m *Manager) health() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", Port), nil)
	if k, err := m.Key(); err == nil {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var h struct {
		Device string `json:"device"`
	}
	_ = decodeJSON(resp, &h)
	return h.Device, true
}

func (m *Manager) setBusy(b, errText string) {
	m.mu.Lock()
	m.busy, m.err = b, errText
	m.mu.Unlock()
}

// Install 装运行环境、下载权重，在后台跑；已经在忙就报错。done 在结束时调用（可为 nil）。
func (m *Manager) Install(done func(error)) error {
	m.mu.Lock()
	if m.busy != "" {
		b := m.busy
		m.mu.Unlock()
		return errors.New("Laya 正在" + busyLabel(b) + "，等它结束")
	}
	m.busy, m.err = "installing", ""
	m.mu.Unlock()
	go func() {
		err := m.install()
		if err != nil {
			m.setBusy("", err.Error())
		} else {
			m.setBusy("", "")
		}
		if done != nil {
			done(err)
		}
	}()
	return nil
}

func busyLabel(b string) string {
	switch b {
	case "installing":
		return "安装运行环境"
	case "downloading":
		return "下载模型"
	case "starting":
		return "启动"
	}
	return b
}

func (m *Manager) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = m.Dir
	cmd.Env = m.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if len(tail) > 300 {
			tail = "…" + tail[len(tail)-300:]
		}
		return fmt.Errorf("%s：%s", err, tail)
	}
	return nil
}

func (m *Manager) env() []string {
	env := os.Environ()
	if m.Env != nil {
		env = m.Env()
	}
	return append(env, "HF_HOME="+m.hfHome(), "HF_HUB_DISABLE_TELEMETRY=1", "PYTHONUNBUFFERED=1")
}

func (m *Manager) install() error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if !m.installed() {
		uv, err := exec.LookPath("uv")
		if err != nil {
			return errors.New("安装失败：本机没有 uv，先在终端里执行 brew install uv")
		}
		if err := m.run(ctx, uv, "venv", "--python", "3.12", filepath.Join(m.Dir, "venv")); err != nil {
			return errors.New("建运行环境失败：" + firstLine(err.Error()))
		}
		if err := m.run(ctx, uv, "pip", "install", "--python", m.venvBin("python"), "laya[serve]=="+Version); err != nil {
			return errors.New("装 Laya 失败：" + firstLine(err.Error()))
		}
	}
	if _, ok := m.weights(); !ok {
		m.setBusy("downloading", "")
		py := fmt.Sprintf(`from huggingface_hub import snapshot_download; snapshot_download(%q, allow_patterns=[%q])`, Repo, Checkpoint+"/*")
		if err := m.run(ctx, m.venvBin("python"), "-c", py); err != nil {
			return errors.New("下载模型失败：" + firstLine(err.Error()))
		}
		if _, ok := m.weights(); !ok {
			return errors.New("下载模型失败：没找到权重文件")
		}
	}
	return nil
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "Fetching") && !strings.HasPrefix(l, "Warning") {
			return l
		}
	}
	return strings.TrimSpace(s)
}

// Start 起 laya-serve；已经在跑就不动。等到 /health 通了才返回。
func (m *Manager) Start() error {
	if _, ok := m.health(); ok {
		return nil
	}
	if !m.installed() {
		return errors.New("还没下载 Laya")
	}
	if _, ok := m.weights(); !ok {
		return errors.New("模型还没下完")
	}
	m.mu.Lock()
	if m.busy != "" {
		b := m.busy
		m.mu.Unlock()
		return errors.New("Laya 正在" + busyLabel(b) + "，等它结束")
	}
	m.busy, m.err = "starting", ""
	m.mu.Unlock()
	err := m.start()
	if err != nil {
		m.setBusy("", err.Error())
		return err
	}
	m.setBusy("", "")
	return nil
}

func (m *Manager) start() error {
	key, err := m.Key()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(m.logFile(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(m.venvBin("laya-serve"))
	cmd.Dir = m.Dir
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = append(m.env(),
		"LAYA_HOST=127.0.0.1", "LAYA_PORT="+strconv.Itoa(Port), "LAYA_API_KEY="+key,
		"LAYA_MODELS="+Checkpoint, "LAYA_DEFAULT_MODEL="+Checkpoint, "LAYA_PRELOAD=1",
		"LAYA_IDLE_UNLOAD_SECONDS="+strconv.Itoa(idleUnload), "LAYA_LOG_LEVEL=warning",
		"HF_HUB_OFFLINE=1") // 权重已经在本地了：启动不再联网
	ownGroup(cmd) // 自成进程组：Switchboard 换版本重启时它接着跑，下次启动认领；停的时候连子进程一起停
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fmt.Errorf("启动失败：%w", err)
	}
	logf.Close()
	_ = os.WriteFile(m.pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.After(startupWait)
	for {
		select {
		case <-exited:
			return errors.New("启动失败：" + m.logTail())
		case <-deadline:
			_ = m.Stop()
			return errors.New("启动失败：90 秒内没有就绪")
		case <-time.After(time.Second):
			if _, ok := m.health(); ok {
				return nil
			}
		}
	}
}

func (m *Manager) logTail() string {
	b, _ := os.ReadFile(m.logFile())
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "INFO") && !strings.HasPrefix(l, "W1") {
			return l
		}
	}
	return "看 " + m.logFile()
}

// Stop 停掉 laya-serve（整个进程组）。
func (m *Manager) Stop() error {
	b, err := os.ReadFile(m.pidFile())
	if err != nil {
		return nil
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 1 {
		stopGroup(pid)
	}
	_ = os.Remove(m.pidFile())
	return nil
}

// Remove 停服务、删掉整个 laya/ 目录。
func (m *Manager) Remove() error {
	m.mu.Lock()
	busy := m.busy
	m.mu.Unlock()
	if busy != "" {
		return errors.New("Laya 正在" + busyLabel(busy) + "，等它结束")
	}
	_ = m.Stop()
	return os.RemoveAll(m.Dir)
}
