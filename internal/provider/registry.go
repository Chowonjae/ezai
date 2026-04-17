package provider

import (
	"fmt"
	"sync"

	"github.com/Chowonjae/ezai/internal/model"
)

// Registry - 프로바이더 등록/조회 레지스트리
// 서버 시작 시 활성화된 프로바이더를 등록하고, 요청 시 이름으로 조회한다.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry - 새 레지스트리 생성
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register - 프로바이더 등록
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get - 이름으로 프로바이더 조회
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("프로바이더 '%s'을(를) 찾을 수 없습니다", name)
	}
	return p, nil
}

// All - 등록된 모든 프로바이더 반환
func (r *Registry) All() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]Provider, len(r.providers))
	for k, v := range r.providers {
		result[k] = v
	}
	return result
}

// AllModels - 모든 프로바이더의 사용 가능한 모델 목록 반환
func (r *Registry) AllModels() []model.ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var models []model.ModelInfo
	for _, p := range r.providers {
		models = append(models, p.Models()...)
	}
	return models
}
