package sub

import (
	"strings"
	"sync"
	"time"
)

// 官方 CLI 每次启动要加载插件、hooks、配置，claude 约 6 秒、agy 约 17 秒，模型本身只要一两秒。
// 预热池提前按同样的参数启动一个进程等在标准输入上，任务来了直接写进去：
// 实测 claude Haiku 9.3 → 1.8 秒，agy flash-low 21 → 4 秒。
// 每个进程只接一个任务，用完就退出，不同任务的上下文不会混在一起；空闲时不调模型、不耗额度。

// warmPool 按启动参数存预热好的进程。
type warmPool struct {
	mu      sync.Mutex
	idle    time.Duration // 闲置多久就关掉
	max     int           // 最多同时留几个（每个 claude 进程常驻约 230 MB）
	procs   map[string]*warmEntry
	pending map[string]bool
	closed  bool
}

type warmEntry struct {
	p     *started
	timer *time.Timer
}

func newWarmPool(idle time.Duration, max int) *warmPool {
	return &warmPool{idle: idle, max: max, procs: map[string]*warmEntry{}, pending: map[string]bool{}}
}

func warmKey(spec procSpec) string {
	return spec.Bin + "\x00" + spec.Dir + "\x00" + strings.Join(spec.Args, "\x00")
}

// take 取出参数完全相同、还活着的预热进程；没有返回 nil。
func (w *warmPool) take(spec procSpec) *started {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	k := warmKey(spec)
	e, ok := w.procs[k]
	if !ok {
		return nil
	}
	delete(w.procs, k)
	e.timer.Stop()
	if !e.p.alive() {
		go e.p.kill()
		return nil
	}
	return e.p
}

// refill 在后台按同样的参数补一个预热进程。已经有、正在补、池满或已关闭时什么也不做。
func (w *warmPool) refill(spec procSpec) {
	if w == nil {
		return
	}
	k := warmKey(spec)
	w.mu.Lock()
	if w.closed || w.pending[k] || w.procs[k] != nil || len(w.procs)+len(w.pending) >= w.max {
		w.mu.Unlock()
		return
	}
	w.pending[k] = true
	w.mu.Unlock()
	go func() {
		p, err := startProc(spec)
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.pending, k)
		if err != nil {
			return
		}
		if w.closed || w.procs[k] != nil {
			go p.kill()
			return
		}
		e := &warmEntry{p: p}
		e.timer = time.AfterFunc(w.idle, func() { w.expire(k, e) })
		w.procs[k] = e
	}()
}

func (w *warmPool) expire(k string, e *warmEntry) {
	w.mu.Lock()
	if w.procs[k] != e {
		w.mu.Unlock()
		return
	}
	delete(w.procs, k)
	w.mu.Unlock()
	e.p.kill()
}

// size 是当前预热好的进程数（测试和状态展示用）。
func (w *warmPool) size() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.procs)
}

// close 关掉所有预热进程，之后不再补。
func (w *warmPool) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	procs := w.procs
	w.procs = map[string]*warmEntry{}
	w.mu.Unlock()
	for _, e := range procs {
		e.timer.Stop()
		e.p.kill()
	}
}
