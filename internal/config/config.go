// Package config 读写数据目录（macOS 的 ~/Library/Application Support/Switchboard，Windows 的 %AppData%\Switchboard）下的 config.yaml，
// 并提供带默认值的配置结构。界面改的每一项都落到这里。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	RulesOnly            bool          `yaml:"rules_only,omitempty" json:"rules_only"` // 只按规则判档，拿不准时也不问低档模型
	Currency             string        `yaml:"currency,omitempty" json:"currency"`     // 费用显示用的币种：CNY 或 USD；账本始终按美元记
}

// CNY 表示显示人民币。
func (s Settings) CNY() bool { return s.Currency == "CNY" }

// Money 把美元金额按显示币种格式化：不到一分的小额多留两位，免得全显示成 0.00。
func (s Settings) Money(usd float64) string {
	sym, v := "$", usd
	if s.CNY() && s.USDPerCNY > 0 {
		sym, v = "¥", usd/s.USDPerCNY
	}
	return sym + moneyDigits(v)
}

// MoneyNative 用于有原币金额的记录：显示人民币时人民币账号直接用账单原价，不来回换算。
func (s Settings) MoneyNative(usd, native float64, currency string) string {
	if s.CNY() && currency == "CNY" && native > 0 {
		return "¥" + moneyDigits(native)
	}
	return s.Money(usd)
}

func moneyDigits(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v < 0.01 && v > -0.01:
		return strconv.FormatFloat(v, 'f', 4, 64)
	default:
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
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
	Plan     string            `yaml:"plan,omitempty" json:"plan,omitempty"` // 手填的套餐名，命令行读不到时用
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
	// AllowPaid 为假时只用平台当前免费的型号（目前只有 Agnes 有免费型号）
	AllowPaid bool `yaml:"allow_paid,omitempty" json:"allow_paid,omitempty"`
	// Media 是这个账号出图 / 出视频各用哪个型号：image / video → 型号；值为空表示不用；没有这个键表示用默认
	Media map[string]string `yaml:"media,omitempty" json:"media,omitempty"`
}

// MediaModel 返回账号在某种媒体上选的型号；没选过就用默认值 def，明确选了「不用」返回空。
func (a APIAccount) MediaModel(kind, def string) string {
	if a.Media != nil {
		if v, ok := a.Media[kind]; ok {
			return v
		}
	}
	return def
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
	Patterns     []string `yaml:"patterns,omitempty" json:"patterns,omitempty"` // 正则，任一命中；和 keywords 二选一命中即可
	MaxChars     int      `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
	MinChars     int      `yaml:"min_chars,omitempty" json:"min_chars,omitempty"`
	HasCode      *bool    `yaml:"has_code,omitempty" json:"has_code,omitempty"`
	HasStack     *bool    `yaml:"has_stack,omitempty" json:"has_stack,omitempty"`
	HasImage     *bool    `yaml:"has_image,omitempty" json:"has_image,omitempty"`
	MaxCodeLines int      `yaml:"max_code_lines,omitempty" json:"max_code_lines,omitempty"`
	LongInput    bool     `yaml:"long_input,omitempty" json:"long_input,omitempty"` // 输入像日志 / 长文
	Exclude      []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`       // 任一命中就不算这一类，挡掉「CSS 动画闪烁」这种借词
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
	RulesVersion int           `yaml:"rules_version,omitempty" json:"rules_version,omitempty"` // 内置任务表的版本，升级时据此合并新规则
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
	// macOS 是 ~/Library/Application Support，Windows 是 %AppData%，Linux 是 ~/.config
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, AppName)
	legacy := filepath.Join(base, legacyAppName)
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
		cfg.Routing.RulesVersion = 0 // 老文件里没有这个字段，不能沿用默认值，否则升级合并不会跑
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("配置文件无法解析：%w", err)
		}
		was := cfg.Routing.RulesVersion
		fillMissing(cfg)
		s.cfg = cfg
		if was != cfg.Routing.RulesVersion {
			// 规则合并要落盘，否则每次启动都会再合一遍，用户删掉的新行又冒出来
			if err := s.save(); err != nil {
				return nil, err
			}
		}
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

// rulesAddedIn 记录每一版新增的内置任务类型：只在升级那一次补进去，用户之后删掉的不再补。
var rulesAddedIn = map[int][]string{2: {"hard_problem", "code_write", "tech_issue", "explain"}}

// mergeRules 把新版内置任务表合进老配置。识别说明没改过的内置行整行换成新规则（保留用户选的档位和渠道）；
// 改过的只补排除词；新增的类型按默认顺序插进去。
func mergeRules(c, d *Config) {
	if c.Routing.RulesVersion >= RulesVersion {
		return
	}
	defs := map[string]TaskType{}
	for _, t := range d.Routing.TaskTable {
		defs[t.Type] = t
	}
	have := map[string]bool{}
	for i, t := range c.Routing.TaskTable {
		have[t.Type] = true
		dt, ok := defs[t.Type]
		if !ok {
			continue
		}
		if t.Hint == dt.Hint {
			c.Routing.TaskTable[i].Match = dt.Match
			c.Routing.TaskTable[i].Order = dt.Order
		} else if len(t.Match.Exclude) == 0 {
			c.Routing.TaskTable[i].Match.Exclude = dt.Match.Exclude
		}
	}
	for v := c.Routing.RulesVersion + 1; v <= RulesVersion; v++ {
		for _, typ := range rulesAddedIn[v] {
			if !have[typ] {
				c.Routing.TaskTable = append(c.Routing.TaskTable, defs[typ])
				have[typ] = true
			}
		}
	}
	c.Routing.RulesVersion = RulesVersion
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
	// DeepSeek 官方型号名改过：deepseek-v4.1-flash 已不被接受，统一换成 deepseek-flash
	for i := range c.API {
		for tier, list := range c.API[i].Models {
			for j, m := range list {
				if m == "deepseek-v4.1-flash" {
					c.API[i].Models[tier][j] = "deepseek-flash"
				}
			}
		}
	}
	// 内置任务类型新增的正则匹配：老配置里没有就补上，不动用户改过的关键词
	for i := range c.Routing.TaskTable {
		if len(c.Routing.TaskTable[i].Match.Patterns) > 0 {
			continue
		}
		for _, dt := range d.Routing.TaskTable {
			if dt.Type == c.Routing.TaskTable[i].Type && len(dt.Match.Patterns) > 0 {
				c.Routing.TaskTable[i].Match.Patterns = append([]string(nil), dt.Match.Patterns...)
			}
		}
	}
	mergeRules(c, d)
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

// ApplyFreeOnly 在只用免费型号时把收费型号从各档位里摘掉；允许收费时不动已有选择。
func ApplyFreeOnly(a *APIAccount, isFree func(string) bool) {
	if a.AllowPaid {
		return
	}
	for tier, list := range a.Models {
		keep := list[:0]
		for _, m := range list {
			if isFree(m) {
				keep = append(keep, m)
			}
		}
		a.Models[tier] = keep
	}
}

// CheckHostKey 校验门面密钥。允许在密钥后加「.宿主名」做标记（imls-xxx.pi），返回标出的宿主；
// 宿主名只认小写字母、数字和下划线，最长 32 个字符。
func CheckHostKey(got, key string) (host string, ok bool) {
	if key == "" || got == "" {
		return "", false
	}
	if got == key {
		return "", true
	}
	base, tag, found := strings.Cut(got, key+".")
	if !found || base != "" || tag == "" || len(tag) > 32 {
		return "", false
	}
	for _, r := range tag {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "", false
		}
	}
	return tag, true
}
