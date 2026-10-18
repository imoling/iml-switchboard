// Package core 定义调度器的共享类型：任务、决策、渠道驱动接口。
// 渠道实现放在 internal/lanes/*，它们只依赖这里的接口，不反向依赖调度器。
package core

import (
	"context"
	"encoding/json"
	"time"
)

// Kind 是任务种类。
type Kind string

const (
	KindText      Kind = "text"
	KindImage     Kind = "image"
	KindVideo     Kind = "video"
	KindAnimation Kind = "animation"
)

// Tier 是文本档位。路由规则只在 low / mid / high 里选；top（Fable）只由显式指令、重做、质量模式触发。
type Tier string

const (
	TierLow  Tier = "low"
	TierMid  Tier = "mid"
	TierHigh Tier = "high"
	TierTop  Tier = "top"
)

// LaneID 是渠道标识。
type LaneID string

const (
	LaneClaudeSub LaneID = "claude_sub"
	LaneCodexSub  LaneID = "codex_sub"
	LaneAgySub    LaneID = "agy_sub"
	LaneAPI       LaneID = "api"
	LaneMedia     LaneID = "media"
)

// Mode 是调度模式：省钱、均衡、质量。
type Mode string

const (
	ModeSave     Mode = "save"
	ModeBalanced Mode = "balanced"
	ModeQuality  Mode = "quality"
)

// Message 是对话消息的最小表示，门面接口会把 OpenAI 格式转成它。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Images 是本地文件路径或 data URL，按量付费渠道会转成供应商格式。
	Images []string `json:"images,omitempty"`
}

// Task 是三个接入面统一后的请求。
type Task struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	Prompt    string          `json:"prompt"`
	Messages  []Message       `json:"messages,omitempty"` // 多轮时存在；Prompt 是最后一条用户消息
	System    string          `json:"system,omitempty"`
	Tools     json.RawMessage `json:"tools,omitempty"`      // OpenAI 格式的 tools，非空表示 has_tools
	Tier      Tier            `json:"tier,omitempty"`       // 调用方显式指定的档位
	MediaTier string          `json:"media_tier,omitempty"` // cheap default best free
	Model     string          `json:"model,omitempty"`      // 调用方点名的模型
	Lane      LaneID          `json:"lane,omitempty"`       // 调用方点名的渠道
	SessionID string          `json:"session_id,omitempty"`
	Source    string          `json:"source"` // mcp facade cli quick redo
	Host      string          `json:"host,omitempty"`
	EstTokens int             `json:"est_tokens"`
	// 媒体参数
	ImagePath  string    `json:"image_path,omitempty"`
	Seconds    int       `json:"seconds,omitempty"`
	Size       string    `json:"size,omitempty"`
	OutDir     string    `json:"out_dir,omitempty"`
	Engine     string    `json:"engine,omitempty"`
	ProjectDir string    `json:"project_dir,omitempty"` // 宿主的工作目录，媒体产物落到它的 assets/
	Stream     bool      `json:"stream,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// HasTools 表示调用方传了工具定义；这类请求不能走订阅渠道。
func (t *Task) HasTools() bool {
	return len(t.Tools) > 0 && string(t.Tools) != "null" && string(t.Tools) != "[]"
}

// Target 是一个候选执行目标。
type Target struct {
	Lane       LaneID  `json:"lane"`
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	AccountID  string  `json:"account_id,omitempty"`
	EstCostUSD float64 `json:"est_cost_usd"`
	Skipped    string  `json:"skipped,omitempty"` // 非空表示被跳过及原因
}

// Decision 是路由结果。
type Decision struct {
	TaskType   string   `json:"task_type"`
	Kind       Kind     `json:"kind"`
	Tier       Tier     `json:"tier,omitempty"`
	MediaTier  string   `json:"media_tier,omitempty"`
	Candidates []Target `json:"candidates"`
	Reason     []string `json:"reason"`
	Sticky     bool     `json:"sticky,omitempty"`
}

// Usage 是一次执行的用量与费用。
type Usage struct {
	InputTokens     int     `json:"input_tokens,omitempty"`
	OutputTokens    int     `json:"output_tokens,omitempty"`
	CacheReadTokens int     `json:"cache_read_tokens,omitempty"`
	Units           float64 `json:"units,omitempty"` // 图片张数 / 视频秒数
	Unit            string  `json:"unit,omitempty"`  // image second
	CostUSD         float64 `json:"cost_usd"`
	CostNative      float64 `json:"cost_native,omitempty"`
	Currency        string  `json:"currency,omitempty"`
	QuotaUnits      float64 `json:"quota_units,omitempty"` // 订阅渠道的额度占用估算
}

// Event 是执行过程中的流式事件。
type Event struct {
	Type string `json:"type"` // text_delta media job done error status
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
	Job  *Job   `json:"job,omitempty"`
	Err  string `json:"error,omitempty"`
}

// Result 是执行的最终结果。
type Result struct {
	Text  string          `json:"text,omitempty"`
	Path  string          `json:"path,omitempty"`
	JobID string          `json:"job_id,omitempty"`
	Model string          `json:"model"`
	Usage Usage           `json:"usage"`
	Raw   json.RawMessage `json:"-"`
}

// Job 是异步媒体任务（视频、动画）。
type Job struct {
	ID            string    `json:"id"`
	Kind          Kind      `json:"kind"`
	Status        string    `json:"status"` // queued submitted running done failed canceled timeout
	Progress      float64   `json:"progress"`
	ETASeconds    int       `json:"eta_seconds,omitempty"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	PromptExcerpt string    `json:"prompt_excerpt"`
	ResultPath    string    `json:"result_path,omitempty"`
	Error         string    `json:"error,omitempty"`
	CostUSD       float64   `json:"cost_usd,omitempty"`
	CallID        string    `json:"call_id"`
	CodeDir       string    `json:"code_dir,omitempty"` // 动画：Remotion 工程目录
	RetryOptions  []Target  `json:"retry_options,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// 内部字段
	ProviderRef string `json:"-"`
	Task        *Task  `json:"-"`
}

// Health 是渠道的即时健康状态。
type Health struct {
	Status string `json:"status"` // ok warn missing error cooldown disabled
	Detail string `json:"detail"`
	// Plan 是套餐名（Max 20x、Pro、免费版…），只做展示
	Plan string `json:"plan,omitempty"`
	// 订阅渠道的额度窗口估算
	Quota         []QuotaWindow `json:"quota,omitempty"`
	CooldownUntil time.Time     `json:"cooldown_until,omitempty"`
}

// QuotaWindow 是一个额度窗口。
type QuotaWindow struct {
	Window     string    `json:"window"` // 5h week month week_fable
	Label      string    `json:"label,omitempty"`
	Pct        float64   `json:"pct"`
	ResetsAt   time.Time `json:"resets_at,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"` // 官方 CLI 自报的时间；零值表示是估算
	Estimated  bool      `json:"estimated,omitempty"`
}

// QuotaProber 由能主动探测额度的驱动实现（Claude 的 CLI 每次调用都会回报窗口利用率）。
type QuotaProber interface {
	ProbeQuota(ctx context.Context) ([]QuotaWindow, error)
}

// QuotaProberDeep 是更贵的探测：会用到顶配模型，只在手动刷新时调用。
type QuotaProberDeep interface {
	ProbeQuotaDeep(ctx context.Context) ([]QuotaWindow, error)
}

// ErrRateLimited 由驱动返回，表示该渠道被限流或额度用完。
type ErrRateLimited struct {
	Lane       LaneID
	RetryAfter time.Duration
	Detail     string
}

func (e *ErrRateLimited) Error() string { return string(e.Lane) + " 额度暂时用完：" + e.Detail }

// ErrProvider 是供应商侧的失败，带一行人话和原始回包。
type ErrProvider struct {
	Summary   string // 界面上显示的一行
	Raw       string // 详情
	Retryable bool   // 换一个候选是否有意义
	// Account 为真表示问题出在这个账号（密钥无效、余额不足），该账号先歇着，请求换下一个候选
	Account  bool
	Cooldown time.Duration
}

func (e *ErrProvider) Error() string { return e.Summary }

// Driver 是一条渠道的执行器。订阅渠道一个 CLI 一个驱动；按量付费渠道一个 OpenAI 兼容驱动；媒体渠道一个驱动内部再分供应商。
type Driver interface {
	ID() LaneID
	// Run 执行任务。emit 用于流式事件，可以为 nil。
	Run(ctx context.Context, task *Task, target Target, emit func(Event)) (*Result, error)
	// Health 返回即时状态，不应发起付费请求。
	Health(ctx context.Context) Health
}

// JobStore 由媒体渠道使用，调度器实现。
type JobStore interface {
	Put(job *Job) error
	Get(id string) (*Job, bool)
	List(kind Kind) []*Job
	Update(id string, fn func(*Job)) (*Job, error)
}

// Secrets 是密钥存取，实现放在 keychain。
type Secrets interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}
