package core

import (
	"sync"
	"time"
)

// Sticky 记住每个会话上次用的目标，同一会话不换模型。
type Sticky struct {
	mu   sync.Mutex
	last map[string]stickyEntry
}

type stickyEntry struct {
	Target Target
	Tier   Tier
	At     time.Time
}

// NewSticky 创建会话粘性表。
func NewSticky() *Sticky { return &Sticky{last: map[string]stickyEntry{}} }

// Get 返回会话上次的目标（30 分钟内有效）。
func (s *Sticky) Get(session string) (Target, Tier, bool) {
	if session == "" {
		return Target{}, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.last[session]
	if !ok || time.Since(e.At) > 30*time.Minute {
		delete(s.last, session)
		return Target{}, "", false
	}
	return e.Target, e.Tier, true
}

// Set 记录会话目标。
func (s *Sticky) Set(session string, t Target, tier Tier) {
	if session == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last[session] = stickyEntry{Target: t, Tier: tier, At: time.Now()}
}

// Forget 清掉会话。
func (s *Sticky) Forget(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.last, session)
}
