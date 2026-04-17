package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Chowonjae/ezai/internal/crypto"
	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/store"
)

const (
	clientKeyCachePrefix = "ezai:clientkey:"
	clientKeyCacheTTL    = 5 * time.Minute
)

// ClientKeyService - 클라이언트 키 검증 서비스
// middleware.ClientKeyValidator 인터페이스를 구현한다.
type ClientKeyService struct {
	store *store.ClientKeyStore
	redis *redis.Client // nil이면 캐시 없이 DB 직접 조회
}

// NewClientKeyService - 검증 서비스 생성
func NewClientKeyService(store *store.ClientKeyStore, redis *redis.Client) *ClientKeyService {
	return &ClientKeyService{store: store, redis: redis}
}

// Validate - 클라이언트 키 검증
// client_id로 조회 → secret 해시 비교 → 만료/활성 확인
func (s *ClientKeyService) Validate(clientID, secret string) (*middleware.ValidatedKey, error) {
	key, err := s.store.GetByClientID(clientID)
	if err != nil {
		return nil, fmt.Errorf("키 조회 실패")
	}
	if key == nil {
		return nil, fmt.Errorf("유효하지 않은 클라이언트 ID")
	}

	if !key.IsActive {
		return nil, fmt.Errorf("비활성화된 키입니다")
	}

	// 만료 확인
	expiresAt, err := time.Parse(time.RFC3339, key.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("만료 시간 파싱 실패")
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, fmt.Errorf("만료된 키입니다")
	}

	// 시크릿 해시 비교 (timing-safe)
	if !crypto.CompareSecretHash(secret, key.SecretHash) {
		return nil, fmt.Errorf("유효하지 않은 시크릿")
	}

	return &middleware.ValidatedKey{
		ClientID:    key.ClientID,
		ServiceName: key.ServiceName,
		ExpiresAt:   expiresAt,
	}, nil
}

// InvalidateCache - 캐시 무효화 (로테이션/비활성화 시 호출)
func (s *ClientKeyService) InvalidateCache(clientID string) {
	if s.redis == nil {
		return
	}
	s.redis.Del(context.Background(), clientKeyCachePrefix+clientID)
}
