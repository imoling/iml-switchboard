package core

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// MemJobStore 是内存任务表，变化写回账本。
type MemJobStore struct {
	mu     sync.RWMutex
	jobs   map[string]*Job
	ledger *Ledger
	onChange func(*Job)
}

// NewJobStore 创建任务表，并从账本恢复。
func NewJobStore(l *Ledger, onChange func(*Job)) *MemJobStore {
	s := &MemJobStore{jobs: map[string]*Job{}, ledger: l, onChange: onChange}
	if l != nil {
		if saved, err := l.LoadJobs(); err == nil {
			for _, j := range saved {
				if j.Status == "queued" || j.Status == "submitted" || j.Status == "running" {
					j.Status = "failed"
					j.Error = "服务重启，任务中断"
					j.UpdatedAt = time.Now()
					_ = l.SaveJob(j)
				}
				s.jobs[j.ID] = j
			}
		}
	}
	return s
}

// Put 写入。
func (s *MemJobStore) Put(j *Job) error {
	s.mu.Lock()
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now()
	}
	j.UpdatedAt = time.Now()
	s.jobs[j.ID] = j
	s.mu.Unlock()
	if s.ledger != nil {
		_ = s.ledger.SaveJob(j)
	}
	if s.onChange != nil {
		s.onChange(j)
	}
	return nil
}

// Get 读取。
func (s *MemJobStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// List 按时间倒序列出；kind 为空列全部。
func (s *MemJobStore) List(kind Kind) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*Job{}
	for _, j := range s.jobs {
		if kind == "" || j.Kind == kind {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CreatedAt.After(out[k].CreatedAt) })
	return out
}

// Update 在锁内修改。
func (s *MemJobStore) Update(id string, fn func(*Job)) (*Job, error) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return nil, errors.New("任务不存在")
	}
	fn(j)
	j.UpdatedAt = time.Now()
	cp := *j
	s.mu.Unlock()
	if s.ledger != nil {
		_ = s.ledger.SaveJob(&cp)
	}
	if s.onChange != nil {
		s.onChange(&cp)
	}
	return &cp, nil
}
