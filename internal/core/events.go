package core

import (
	"encoding/json"
	"sync"
	"time"
)

// Bus 是给界面的事件总线：请求、任务、额度、通知、状态。
type Bus struct {
	mu     sync.Mutex
	subs   map[chan Envelope]struct{}
	recent []Envelope
}

// Envelope 是一条事件。
type Envelope struct {
	Type string          `json:"type"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// NewBus 创建总线。
func NewBus() *Bus { return &Bus{subs: map[chan Envelope]struct{}{}} }

// PublishCall、PublishJob 按 docs/API.md 的约定包一层：界面读的是 {call} 和 {job}，发裸对象它拿到的就是 undefined，
// 浮窗渲染时直接崩成空白。
func (b *Bus) PublishCall(c *Call) { b.Publish("call", map[string]any{"call": c}) }

// PublishJob 见 PublishCall。
func (b *Bus) PublishJob(j *Job) { b.Publish("job", map[string]any{"job": j}) }

// CallStart 是一次请求开始交给某个渠道；换候选时用同一个 ID 再发一次。做完发的是 call 事件。
type CallStart struct {
	ID       string `json:"id"`
	Host     string `json:"host,omitempty"`
	Lane     LaneID `json:"lane"`
	Provider string `json:"provider"`
}

// PublishCallStart 广播 call_start。
func (b *Bus) PublishCallStart(c CallStart) { b.Publish("call_start", c) }

// Publish 广播。
func (b *Bus) Publish(typ string, data any) {
	raw, _ := json.Marshal(data)
	env := Envelope{Type: typ, At: time.Now(), Data: raw}
	b.mu.Lock()
	b.recent = append(b.recent, env)
	if len(b.recent) > 50 {
		b.recent = b.recent[len(b.recent)-50:]
	}
	for ch := range b.subs {
		select {
		case ch <- env:
		default:
		}
	}
	b.mu.Unlock()
}

// Subscribe 订阅；返回取消函数。
func (b *Bus) Subscribe() (<-chan Envelope, func()) {
	ch := make(chan Envelope, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Notice 是一条系统通知。
type Notice struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Level string `json:"level"` // info warn error
}
