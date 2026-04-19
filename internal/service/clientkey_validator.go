package service

import (
	"context"
	"encoding/json"
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

// cachedKey - Redis에 캐시할 키 정보
type cachedKey struct {
	SecretHash  string `json:"secret_hash"`
	ServiceName string `json:"service_name"`
	IsActive    bool   `json:"is_active"`
	ExpiresAt   string `json:"expires_at"`
}

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
// Redis 캐시 → 미스 시 DB 조회 → 캐시 저장 → secret 해시 비교 → 만료/활성 확인
func (s *ClientKeyService) Validate(clientID, secret string) (*middleware.ValidatedKey, error) {
	var secretHash, serviceName, expiresAtStr string
	var isActive bool

	// 1. Redis 캐시 조회
	if cached, err := s.getFromCache(clientID); err == nil && cached != nil {
		secretHash = cached.SecretHash
		serviceName = cached.ServiceName
		isActive = cached.IsActive
		expiresAtStr = cached.ExpiresAt
	} else {
		// 2. 캐시 미스: DB 조회
		key, err := s.store.GetByClientID(clientID)
		if err != nil {
			return nil, fmt.Errorf("키 조회 실패: %w", err)
		}
		if key == nil {
			return nil, fmt.Errorf("유효하지 않은 클라이언트 ID")
		}
		secretHash = key.SecretHash
		serviceName = key.ServiceName
		isActive = key.IsActive
		expiresAtStr = key.ExpiresAt

		// 3. 캐시 저장
		s.saveToCache(clientID, &cachedKey{
			SecretHash:  key.SecretHash,
			ServiceName: key.ServiceName,
			IsActive:    key.IsActive,
			ExpiresAt:   key.ExpiresAt,
		})
	}

	if !isActive {
		return nil, fmt.Errorf("비활성화된 키입니다")
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return nil, fmt.Errorf("만료 시간 파싱 실패: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, fmt.Errorf("만료된 키입니다")
	}

	// 시크릿 해시 비교 (timing-safe)
	if !crypto.CompareSecretHash(secret, secretHash) {
		return nil, fmt.Errorf("유효하지 않은 시크릿")
	}

	return &middleware.ValidatedKey{
		ClientID:    clientID,
		ServiceName: serviceName,
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *ClientKeyService) getFromCache(clientID string) (*cachedKey, error) {
	if s.redis == nil {
		return nil, nil
	}
	data, err := s.redis.Get(context.Background(), clientKeyCachePrefix+clientID).Bytes()
	if err != nil {
		return nil, err
	}
	var ck cachedKey
	if err := json.Unmarshal(data, &ck); err != nil {
		return nil, err
	}
	return &ck, nil
}

func (s *ClientKeyService) saveToCache(clientID string, ck *cachedKey) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(ck)
	if err != nil {
		return
	}
	s.redis.Set(context.Background(), clientKeyCachePrefix+clientID, data, clientKeyCacheTTL)
}

// InvalidateCache - 캐시 무효화 (로테이션/비활성화 시 호출)
func (s *ClientKeyService) InvalidateCache(clientID string) {
	if s.redis == nil {
		return
	}
	s.redis.Del(context.Background(), clientKeyCachePrefix+clientID)
}
