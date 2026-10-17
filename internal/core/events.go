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
