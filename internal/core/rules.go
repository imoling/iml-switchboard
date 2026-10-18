package core

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/imoling/iml-switchboard/internal/config"
)

// Signals 是从请求里提取的确定性信号，决策顺序里「启发式」那一步也只看它们。
type Signals struct {
	Chars       int  `json:"chars"`
	CodeBlocks  int  `json:"code_blocks"`
	CodeLines   int  `json:"code_lines"`
	HasStack    bool `json:"has_stack"`
	HasImage    bool `json:"has_image"`
	LongInput   bool `json:"long_input"`
	Constraints int  `json:"constraints"`
	Reasoning   bool `json:"reasoning"`
}

var (
	reFence      = regexp.MustCompile("(?s)```.*?```")
	reStack      = regexp.MustCompile(`(?m)(Traceback \(most recent call last\)|^\s+at [\w$.<>]+ \(|Exception in thread|panic: |goroutine \d+ \[|Error: .*\n\s+at |^\s*File ".*", line \d+|\w+Error: |\w+Exception: |segmentation fault|stack trace)`)
	reCodeish    = regexp.MustCompile(`(?m)^\s*(func |def |class |import |#include|const |let |var |public |private |return |if \(|for \(|\}\s*$)`)
	reConstraint = regexp.MustCompile(`(必须|不要|不能|要求|至少|最多|只能|禁止|务必|不得|需要满足|should|must|never|always)`)
	reReasoning  = regexp.MustCompile(`(为什么|证明|推导|原因是什么|权衡|对比分析|利弊|trade-?off|why does|prove)`)
)

// Extract 提取信号。只看文本，不调用任何模型。
func Extract(prompt string, images int, estTokens int) Signals {
	s := Signals{Chars: utf8.RuneCountInString(prompt), HasImage: images > 0}
	fences := reFence.FindAllString(prompt, -1)
	s.CodeBlocks = len(fences)
	for _, f := range fences {
		s.CodeLines += strings.Count(f, "\n") - 1
	}
	if s.CodeBlocks == 0 {
		// 没有围栏但看起来像代码的行数
		if n := len(reCodeish.FindAllString(prompt, -1)); n >= 3 {
			s.CodeBlocks = 1
			s.CodeLines = n
		}
	}
	s.HasStack = reStack.MatchString(prompt)
	s.LongInput = estTokens > 3000 || s.Chars > 6000
	s.Constraints = len(reConstraint.FindAllString(prompt, -1))
	s.Reasoning = reReasoning.MatchString(prompt)
	return s
}

var reSeconds = regexp.MustCompile(`(\d{1,3})\s*(秒|s\b|sec)`)

// ParseSeconds 从提示词里读「N 秒」，没有就返回 0。
func ParseSeconds(prompt string) int {
	m := reSeconds.FindStringSubmatch(strings.ToLower(prompt))
	if len(m) < 2 {
		return 0
	}
	n := 0
	for _, r := range m[1] {
		n = n*10 + int(r-'0')
	}
	return n
}

// EstimateTokens 粗估 token 数：中文按字数除 1.6，其余按字符除 4。
func EstimateTokens(text string) int {
	cjk, other := 0, 0
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			cjk++
		} else {
			other++
		}
	}
	return int(float64(cjk)/1.6) + other/4
}

// MatchTaskType 按 Order 顺序找第一个条件全满足的任务类型。
func MatchTaskType(table []config.TaskType, prompt string, sig Signals) (config.TaskType, bool) {
	rows := append([]config.TaskType(nil), table...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	lower := strings.ToLower(prompt)
	for _, t := range rows {
		if matches(t.Match, lower, sig) {
			return t, true
		}
	}
	return config.TaskType{}, false
}

func matches(m config.Match, lower string, sig Signals) bool {
	if len(m.Keywords) > 0 {
		hit := false
		for _, k := range m.Keywords {
			if strings.Contains(lower, strings.ToLower(k)) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if m.MaxChars > 0 && sig.Chars > m.MaxChars {
		return false
	}
	if m.MinChars > 0 && sig.Chars < m.MinChars {
		return false
	}
	if m.HasCode != nil && (*m.HasCode != (sig.CodeBlocks > 0)) {
		return false
	}
	if m.HasStack != nil && (*m.HasStack != sig.HasStack) {
		return false
	}
	if m.HasImage != nil && (*m.HasImage != sig.HasImage) {
		return false
	}
	if m.MaxCodeLines > 0 && sig.CodeLines > m.MaxCodeLines {
		return false
	}
	if m.LongInput && !sig.LongInput {
		return false
	}
	return true
}

// Heuristic 是规则表没命中时的兜底打分：返回档位和理由。
func Heuristic(sig Signals) (Tier, string) {
	score := 0
	why := []string{}
	if sig.HasStack {
		score += 3
		why = append(why, "有报错栈")
	}
	if sig.CodeLines > 80 {
		score += 2
		why = append(why, "代码超过 80 行")
	} else if sig.CodeBlocks > 0 {
		score++
		why = append(why, "有代码块")
	}
	if sig.Constraints >= 3 {
		score++
		why = append(why, "约束条件多")
	}
	if sig.Reasoning {
		score++
		why = append(why, "要求推理")
	}
	if sig.Chars > 1500 {
		score++
		why = append(why, "内容长")
	}
	switch {
	case score >= 4:
		return TierHigh, "启发式：" + strings.Join(why, "、")
	case score >= 2:
		return TierMid, "启发式：" + strings.Join(why, "、")
	default:
		if len(why) == 0 {
			why = append(why, "短、无代码、无约束")
		}
		return TierLow, "启发式：" + strings.Join(why, "、")
	}
}

// ExplicitHit 是显式指令命中的结果。
type ExplicitHit struct {
	Tier      Tier
	SubOnly   bool
	MediaBest bool
	Word      string
	Prompt    string // 去掉斜杠指令后的提示词
}

// FindExplicit 在提示词里找显式指令。斜杠指令只认开头或独立成词；中文短语认包含。
func FindExplicit(rules []config.Explicit, prompt string) ExplicitHit {
	hit := ExplicitHit{Prompt: prompt}
	lower := strings.ToLower(prompt)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		for _, w := range r.Words {
			lw := strings.ToLower(w)
			found := false
			if strings.HasPrefix(lw, "/") {
				for _, tok := range strings.Fields(lower) {
					if tok == lw {
						found = true
						break
					}
				}
				if found {
					hit.Prompt = strings.TrimSpace(strings.ReplaceAll(hit.Prompt, w, ""))
				}
			} else {
				found = strings.Contains(lower, lw)
			}
			if !found {
				continue
			}
			hit.Word = w
			switch {
			case r.Tier != "":
				// 多个命中取最高档
				if tierRank(Tier(r.Tier)) > tierRank(hit.Tier) {
					hit.Tier = Tier(r.Tier)
				}
			case r.Effect == "sub_only":
				hit.SubOnly = true
			case r.Effect == "media_best":
				hit.MediaBest = true
			}
		}
	}
	return hit
}

func tierRank(t Tier) int {
	switch t {
	case TierLow:
		return 1
	case TierMid:
		return 2
	case TierHigh:
		return 3
	case TierTop:
		return 4
	}
	return 0
}

// TierLabel 是档位的标准中文名（不走主题）。
func TierLabel(t Tier) string {
	switch t {
	case TierLow:
		return "低档"
	case TierMid:
		return "中档"
	case TierHigh:
		return "高档"
	case TierTop:
		return "顶配"
	}
	return string(t)
}

var reminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// mediaAskLimit 是「直接要图 / 要视频」的长度上限：超过它的多半是宿主塞进来的上下文，不是一句请求。
const mediaAskLimit = 400

// StripInjected 去掉宿主注入的提示块（<system-reminder>…</system-reminder>），只留用户自己说的话。
// 智能体宿主常把技能清单、系统提醒整段塞进用户消息，里面随便一个「动画」「视频」都不该参与路由判断。
func StripInjected(prompt string) string {
	return strings.TrimSpace(reminderRe.ReplaceAllString(prompt, ""))
}

// IsMediaAsk 判断这段话像不像一句直接的媒体请求：去掉注入块后不超过 400 字。
func IsMediaAsk(prompt string) bool {
	return len([]rune(StripInjected(prompt))) <= mediaAskLimit
}
