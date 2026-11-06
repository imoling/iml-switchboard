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

// CallStart 是一次请求开始交给某个渠道；换候选时用同一个 ID 再发一次（Attempt 加一）。做完发的是 call 事件。
// Model 往后的字段给学院页演「谁接的活」：老界面只认前四个，多出来的不影响。
type CallStart struct {
	ID       string `json:"id"`
	Host     string `json:"host,omitempty"`
	Lane     LaneID `json:"lane"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Display  string `json:"display,omitempty"`
	Kind     Kind   `json:"kind,omitempty"`
	Tier     Tier   `json:"tier,omitempty"`
	RuleTier Tier   `json:"rule_tier,omitempty"` // 智能分派改了档时，规则原本判的档
	Judge    string `json:"judge,omitempty"`
	TaskType string `json:"task_type,omitempty"`
	TaskName string `json:"task_name,omitempty"`
	Session  string `json:"session_id,omitempty"`
	Stream   bool   `json:"stream,omitempty"`
	// ToolTurn 为真表示这一轮是应用把工具结果送回来，不是用户新说的话
	ToolTurn  bool `json:"tool_turn,omitempty"`
	Sticky    bool `json:"sticky,omitempty"`
	Attempt   int  `json:"attempt,omitempty"`
	EstTokens int  `json:"est_tokens,omitempty"`
}

// Bounce 是兼容入口把请求原样退回应用（谁都装不下），应用会自己压缩后重发。
type Bounce struct {
	Host      string `json:"host,omitempty"`
	Session   string `json:"session_id,omitempty"`
	EstTokens int    `json:"est_tokens"`
	MaxTokens int    `json:"max_tokens,omitempty"`
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
