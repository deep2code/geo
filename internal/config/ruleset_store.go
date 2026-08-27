package config

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// RuleSetVersion 规则集版本记录。
type RuleSetVersion struct {
	ID          string    `json:"id"`
	RuleSet     *RuleSet  `json:"ruleset"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by,omitempty"`
	Description string    `json:"description,omitempty"`
}

// RuleSetStore 规则集版本存储（内存实现，支持 CRUD）。
type RuleSetStore struct {
	mu       sync.RWMutex
	versions map[string]*RuleSetVersion
	activeID string // 当前激活的版本 ID
}

// NewRuleSetStore 创建规则集存储，自动加载内置默认版本。
func NewRuleSetStore() *RuleSetStore {
	s := &RuleSetStore{
		versions: make(map[string]*RuleSetVersion),
	}
	// 内置默认版本
	s.versions["builtin-1.0.0"] = &RuleSetVersion{
		ID:          "builtin-1.0.0",
		RuleSet:     DefaultRuleSet(),
		CreatedAt:   time.Now(),
		Description: "内置默认规则集",
	}
	s.activeID = "builtin-1.0.0"
	return s
}

// List 返回所有版本（按创建时间降序）。
func (s *RuleSetStore) List() []RuleSetVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RuleSetVersion, 0, len(s.versions))
	for _, v := range s.versions {
		out = append(out, *v)
	}
	// 按时间降序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Get 按 ID 获取版本。
func (s *RuleSetStore) Get(id string) (*RuleSetVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[id]
	return v, ok
}

// GetActive 获取当前激活的版本。
func (s *RuleSetStore) GetActive() (*RuleSetVersion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[s.activeID]
	return v, ok
}

// ActiveID 返回当前激活版本 ID。
func (s *RuleSetStore) ActiveID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeID
}

// Save 保存新版本（JSON 字节 + 元信息）。
func (s *RuleSetStore) Save(id string, rs *RuleSet, createdBy, description string) error {
	if id == "" {
		return fmt.Errorf("版本 ID 不能为空")
	}
	if err := rs.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[id] = &RuleSetVersion{
		ID:          id,
		RuleSet:     rs,
		CreatedAt:   time.Now(),
		CreatedBy:   createdBy,
		Description: description,
	}
	return nil
}

// SaveJSON 从 JSON 字节保存新版本。
func (s *RuleSetStore) SaveJSON(id string, data []byte, createdBy, description string) error {
	var rs RuleSet
	if err := json.Unmarshal(data, &rs); err != nil {
		return fmt.Errorf("解析规则集 JSON 失败: %w", err)
	}
	return s.Save(id, &rs, createdBy, description)
}

// Activate 切换激活版本。
func (s *RuleSetStore) Activate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.versions[id]; !ok {
		return fmt.Errorf("版本 %s 不存在", id)
	}
	s.activeID = id
	return nil
}

// Delete 删除版本（不能删除当前激活版本）。
func (s *RuleSetStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.activeID {
		return fmt.Errorf("不能删除当前激活版本")
	}
	if _, ok := s.versions[id]; !ok {
		return fmt.Errorf("版本 %s 不存在", id)
	}
	delete(s.versions, id)
	return nil
}
