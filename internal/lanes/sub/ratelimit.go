package sub

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultRetryAfter 是解析不出重置时间时的默认冷却时长。
const DefaultRetryAfter = 30 * time.Minute

// rateLimitMarkers 是官方 CLI 限流 / 额度用完时常见的字样。
var rateLimitMarkers = []string{
	"rate limit", "rate_limit", "ratelimit",
	"usage limit", "usage_limit",
	"hit your limit", "limit reached", "quota",
	"resets", "reset at", "try again at", "try again in",
	"too many requests", "429",
	"额度", "限流",
}

// isRateLimitText 判断错误文本是否属于限流 / 额度用完。
func isRateLimitText(s string) bool {
	l := strings.ToLower(s)
	for _, m := range rateLimitMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

var (
	reIn       = regexp.MustCompile(`(?i)(?:in|after)\s+((?:\d+\s*(?:hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b[\s,]*(?:and\s*)?)+)`)
	reInPart   = regexp.MustCompile(`(?i)(\d+)\s*(hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b`)
	reDateTime = regexp.MustCompile(`(?i)(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})(?:,?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)?`)
	reClock    = regexp.MustCompile(`(?i)(?:resets?|reset at|try again at|until)\s*(?:at\s*)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
	reEpoch    = regexp.MustCompile(`"resetsAt"\s*:\s*(\d{10})`)
)

// parseRetryAfter 从错误文本里尽力解析出离重置还有多久；解析不出返回 DefaultRetryAfter 与 false。
func parseRetryAfter(text string, now time.Time) (time.Duration, bool) {
	// 1. 事件里的 epoch
	if m := reEpoch.FindStringSubmatch(text); m != nil {
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			if d := time.Unix(sec, 0).Sub(now); d > 0 {
				return d, true
			}
		}
	}
	// 2. "in 2 hours 15 minutes"
	if m := reIn.FindStringSubmatch(text); m != nil {
		var d time.Duration
		for _, p := range reInPart.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			switch strings.ToLower(p[2][:1]) {
			case "h":
				d += time.Duration(n) * time.Hour
			case "m":
				d += time.Duration(n) * time.Minute
			case "s":
				d += time.Duration(n) * time.Second
			}
		}
		if d > 0 {
			return d, true
		}
	}
	// 3. "Oct 29th, 2026 8:54 AM"
	if m := reDateTime.FindStringSubmatch(text); m != nil {
		month := monthOf(m[1])
		day, _ := strconv.Atoi(m[2])
		year, _ := strconv.Atoi(m[3])
		hour, min := 0, 0
		if m[4] != "" {
			hour, _ = strconv.Atoi(m[4])
			if m[5] != "" {
				min, _ = strconv.Atoi(m[5])
			}
			hour = to24(hour, m[6])
		}
		t := time.Date(year, month, day, hour, min, 0, 0, now.Location())
		if d := t.Sub(now); d > 0 {
			return d, true
		}
	}
	// 4. "resets 3pm" / "resets at 15:00"
	if m := reClock.FindStringSubmatch(text); m != nil {
		hour, _ := strconv.Atoi(m[1])
		min := 0
		if m[2] != "" {
			min, _ = strconv.Atoi(m[2])
		}
		if m[3] == "" && m[2] == "" {
			// 没有 am/pm 也没有分钟的裸数字（如 "resets 5"）不可信
			return DefaultRetryAfter, false
		}
		hour = to24(hour, m[3])
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if !t.After(now) {
			t = t.Add(24 * time.Hour)
		}
		return t.Sub(now), true
	}
	return DefaultRetryAfter, false
}

func to24(hour int, ampm string) int {
	switch strings.ToLower(ampm) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}
	return hour
}

func monthOf(s string) time.Month {
	switch strings.ToLower(s[:3]) {
	case "jan":
		return time.January
	case "feb":
		return time.February
	case "mar":
		return time.March
	case "apr":
		return time.April
	case "may":
		return time.May
	case "jun":
		return time.June
	case "jul":
		return time.July
	case "aug":
		return time.August
	case "sep":
		return time.September
	case "oct":
		return time.October
	case "nov":
		return time.November
	}
	return time.December
}
