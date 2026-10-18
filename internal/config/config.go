// Package config 读写 ~/Library/Application Support/Switchboard/config.yaml，
// 并提供带默认值的配置结构。界面改的每一项都落到这里。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AppName 是数据目录名，也是正式产品名。
const AppName = "Switchboard"

// legacyAppName 是 0.1 之前的数据目录名，启动时自动迁移。
const legacyAppName = "iML Switchboard"

// Settings 对应界面「设置」页。
type Settings struct {
	MonthlyAPILimitUSD   float64       `yaml:"monthly_api_limit_usd" json:"monthly_api_limit_usd"`
	SubscriptionYieldPct float64       `yaml:"subscription_yield_pct" json:"subscription_yield_pct"`
	DefaultMode          string        `yaml:"default_mode" json:"default_mode"`
	Theme                string        `yaml:"theme" json:"theme"`
	Hotkey               string        `yaml:"hotkey" json:"hotkey"`
	Proxy                string        `yaml:"proxy" json:"proxy"`
	SavePrompts          bool          `yaml:"save_prompts" json:"save_prompts"`
	LaunchAtLogin        bool          `yaml:"launch_at_login" json:"launch_at_login"`
	FacadeKey            string        `yaml:"facade_key" json:"facade_key"`
	AutoEvaluateMonthly  bool          `yaml:"auto_evaluate_monthly" json:"auto_evaluate_monthly"`
	Notifications        Notifications `yaml:"notifications" json:"notifications"`
	OnboardingDone       bool          `yaml:"onboarding_done" json:"onboarding_done"`
	USDPerCNY            float64       `yaml:"usd_per_cny" json:"usd_per_cny"`
}

// Notifications 是四类系统通知的开关。
type Notifications struct {
	Quota       bool `yaml:"quota" json:"quota"`
	Budget      bool `yaml:"budget" json:"budget"`
	Media       bool `yaml:"media" json:"media"`
	Suggestions bool `yaml:"suggestions" json:"suggestions"`
}

// SubLane 是一条订阅渠道的配置。
type SubLane struct {
	Enabled  bool              `yaml:"enabled" json:"enabled"`
	Models   map[string]string `yaml:"models" json:"models"` // tier -> model
	ImageGen bool              `yaml:"image_gen,omitempty" json:"image_gen,omitempty"`
}

// APIAccount 是一个按量付费账号。密钥不在这里，在钥匙串里，键名为 account id。
type APIAccount struct {
	ID         string              `yaml:"id" json:"id"`
	Provider   string              `yaml:"provider" json:"provider"`
	Label      string              `yaml:"label,omitempty" json:"label,omitempty"`
	BaseURL    string              `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Currency   string              `yaml:"currency,omitempty" json:"currency,omitempty"`
	Enabled    bool                `yaml:"enabled" json:"enabled"`
	Models     map[string][]string `yaml:"models" json:"models"` // tier -> 候选模型，按优先级
	NeedsProxy bool                `yaml:"needs_proxy,omitempty" json:"needs_proxy,omitempty"`
}

// MediaTarget 是媒体某一档位的目标。
type MediaTarget struct {
	Provider   string `yaml:"provider" json:"provider"` // openrouter ark openai 302ai codex
	Model      string `yaml:"model,omitempty" json:"model,omitempty"`
	Quality    string `yaml:"quality,omitempty" json:"quality,omitempty"`
	Resolution string `yaml:"resolution,omitempty" json:"resolution,omitempty"`
	AccountID  string `yaml:"account_id,omitempty" json:"account_id,omitempty"`
}

// Media 是三种媒体的档位表。
type Media struct {
	Image     map[string]MediaTarget `yaml:"image" json:"image"`
	Video     map[string]MediaTarget `yaml:"video" json:"video"`
	Animation struct {
		CoderLane  string `yaml:"coder_lane" json:"coder_lane"`
		CoderModel string `yaml:"coder_model" json:"coder_model"`
		Renderer   string `yaml:"renderer" json:"renderer"`
	} `yaml:"animation" json:"animation"`
}

// Explicit 是一条显式指令。
type Explicit struct {
	ID      string   `yaml:"id" json:"id"`
	Words   []string `yaml:"words" json:"words"`
	Tier    string   `yaml:"tier,omitempty" json:"tier,omitempty"`     // low mid high top
	Effect  string   `yaml:"effect,omitempty" json:"effect,omitempty"` // sub_only media_best
	Builtin bool     `yaml:"builtin,omitempty" json:"builtin,omitempty"`
	Enabled bool     `yaml:"enabled" json:"enabled"`
}

// ContextRule 是一条上下文规则。
type ContextRule struct {
	ID      string `yaml:"id" json:"id"`
	Title   string `yaml:"title" json:"title"`
	Note    string `yaml:"note" json:"note"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// Match 是任务类型的识别条件；全部可选，设了的都要满足。
type Match struct {
	Keywords     []string `yaml:"keywords,omitempty" json:"keywords,omitempty"` // 任一命中
	MaxChars     int      `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
	MinChars     int      `yaml:"min_chars,omitempty" json:"min_chars,omitempty"`
	HasCode      *bool    `yaml:"has_code,omitempty" json:"has_code,omitempty"`
	HasStack     *bool    `yaml:"has_stack,omitempty" json:"has_stack,omitempty"`
	HasImage     *bool    `yaml:"has_image,omitempty" json:"has_image,omitempty"`
	MaxCodeLines int      `yaml:"max_code_lines,omitempty" json:"max_code_lines,omitempty"`
	LongInput    bool     `yaml:"long_input,omitempty" json:"long_input,omitempty"` // 输入像日志 / 长文
}

// TaskType 是任务类型表的一行。
type TaskType struct {
	Type      string   `yaml:"type" json:"type"`
	Name      string   `yaml:"name" json:"name"`
	Hint      string   `yaml:"hint" json:"hint"` // 界面上「怎么识别」一栏
	Match     Match    `yaml:"match" json:"match"`
	Tier      string   `yaml:"tier,omitempty" json:"tier,omitempty"`
	Kind      string   `yaml:"kind,omitempty" json:"kind,omitempty"` // image video animation；空为 text
	MediaTier string   `yaml:"media_tier,omitempty" json:"media_tier,omitempty"`
	Prefer    []string `yaml:"prefer,omitempty" json:"prefer,omitempty"` // 渠道顺序：api claude_sub codex_sub
	Order     int      `yaml:"order" json:"order"`
}

// Routing 是路由规则。
type Routing struct {
	Sticky       string        `yaml:"sticky" json:"sticky"`
	Explicit     []Explicit    `yaml:"explicit" json:"explicit"`
	ContextRules []ContextRule `yaml:"context_rules" json:"context_rules"`
	TaskTable    []TaskType    `yaml:"task_table" json:"task_table"`
	Ignored      []string      `yaml:"ignored_suggestions,omitempty" json:"ignored_suggestions,omitempty"`
}

// Integration 是一个宿主的接入状态。
type Integration struct {
	Connected bool            `yaml:"connected" json:"connected"`
	Options   map[string]bool `yaml:"options,omitempty" json:"options,omitempty"`
}

// Config 是整份配置。
type Config struct {
	Version      int                    `yaml:"version" json:"version"`
	Settings     Settings               `yaml:"settings" json:"settings"`
	Sub          map[string]SubLane     `yaml:"subscription" json:"subscription"`
	API          []APIAccount           `yaml:"api_accounts" json:"api_accounts"`
	Media        Media                  `yaml:"media" json:"media"`
	Routing      Routing                `yaml:"routing" json:"routing"`
	Integrations map[string]Integration `yaml:"integrations" json:"integrations"`
}

// Store 负责加载、保存和并发访问。
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

// DataDir 返回数据目录，环境变量 IMLS_DATA_DIR 可覆盖（测试用）。
func DataDir() string {
	if d := os.Getenv("IMLS_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "Application Support", AppName)
	legacy := filepath.Join(home, "Library", "Application Support", legacyAppName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if _, err := os.Stat(legacy); err == nil {
			_ = os.Rename(legacy, dir)
		}
	}
	return dir
}

// Open 加载配置；文件不存在就用默认值创建。
func Open() (*Store, error) {
	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.yaml")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.cfg = Default()
		if err := s.save(); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		cfg := Default()
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("配置文件无法解析：%w", err)
		}
		fillMissing(cfg)
		s.cfg = cfg
	}
	if err := Validate(s.cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// Path 返回配置文件路径。
func (s *Store) Path() string { return s.path }

// Get 返回当前配置的只读快照（调用方不要修改）。
func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update 在锁内修改并保存。
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *s.cfg
	if err := fn(&clone); err != nil {
		return err
	}
	if err := Validate(&clone); err != nil {
		return err
	}
	s.cfg = &clone
	return s.save()
}

func (s *Store) save() error {
	b, err := yaml.Marshal(s.cfg)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Validate 检查配置，错在启动时报而不是运行时。
func Validate(c *Config) error {
	switch c.Settings.DefaultMode {
	case "save", "balanced", "quality":
	default:
		return fmt.Errorf("default_mode 只能是 save、balanced、quality，现在是 %q", c.Settings.DefaultMode)
	}
	switch c.Settings.Theme {
	case "standard", "dianjiang", "harem", "seaking":
	default:
		return fmt.Errorf("theme 不认识：%q", c.Settings.Theme)
	}
	if c.Settings.SubscriptionYieldPct < 0 || c.Settings.SubscriptionYieldPct > 100 {
		return errors.New("subscription_yield_pct 要在 0 到 100 之间")
	}
	seen := map[string]bool{}
	for _, t := range c.Routing.TaskTable {
		if t.Type == "" {
			return errors.New("task_table 里有一行没有 type")
		}
		if seen[t.Type] {
			return fmt.Errorf("task_table 里 %q 重复", t.Type)
		}
		seen[t.Type] = true
		if t.Kind == "" {
			switch t.Tier {
			case "low", "mid", "high":
			default:
				return fmt.Errorf("任务类型 %q 的档位只能是 low、mid、high", t.Type)
			}
		}
	}
	for _, e := range c.Routing.Explicit {
		if e.Tier == "" && e.Effect == "" {
			return fmt.Errorf("显式指令 %q 既没有档位也没有效果", e.ID)
		}
	}
	return nil
}

// fillMissing 给旧配置补上后来新增的字段。
func fillMissing(c *Config) {
	d := Default()
	if c.Settings.USDPerCNY == 0 {
		c.Settings.USDPerCNY = d.Settings.USDPerCNY
	}
	if c.Settings.FacadeKey == "" {
		c.Settings.FacadeKey = d.Settings.FacadeKey
	}
	if c.Sub == nil {
		c.Sub = d.Sub
	}
	if c.Integrations == nil {
		c.Integrations = map[string]Integration{}
	}
	if len(c.Routing.TaskTable) == 0 {
		c.Routing.TaskTable = d.Routing.TaskTable
	}
	if len(c.Routing.Explicit) == 0 {
		c.Routing.Explicit = d.Routing.Explicit
	}
	if len(c.Routing.ContextRules) == 0 {
		c.Routing.ContextRules = d.Routing.ContextRules
	}
	if c.Media.Image == nil {
		c.Media.Image = d.Media.Image
	}
	if c.Media.Video == nil {
		c.Media.Video = d.Media.Video
	}
	if c.Media.Animation.Renderer == "" {
		c.Media.Animation = d.Media.Animation
	}
	// 早期版本把 Antigravity 写成停用且型号已过期；检测到旧型号就换成当前默认并启用
	if sl, ok := c.Sub["agy_sub"]; ok {
		if strings.HasPrefix(sl.Models["high"], "gemini-3.1") || len(sl.Models) == 0 {
			c.Sub["agy_sub"] = d.Sub["agy_sub"]
		}
	}
	// 内置显式指令不能删，被删了就补回来
	have := map[string]bool{}
	for _, e := range c.Routing.Explicit {
		have[e.ID] = true
	}
	for _, e := range d.Routing.Explicit {
		if e.Builtin && !have[e.ID] {
			c.Routing.Explicit = append(c.Routing.Explicit, e)
		}
	}
}
