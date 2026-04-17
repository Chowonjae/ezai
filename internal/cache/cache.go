package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Chowonjae/ezai/internal/model"
)

const (
	cacheKeyPrefix = "ezai:cache:" // Redis 키 접두사
	defaultTTL     = 10 * time.Minute
)

// Cache - Redis 기반 응답 캐시
type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewCache - 캐시 생성
func NewCache(rdb *redis.Client, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Cache{rdb: rdb, ttl: ttl}
}

// Get - 캐시에서 응답 조회
// 캐시 히트 시 응답 반환, 미스 시 nil 반환
func (c *Cache) Get(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	key := c.buildKey(req)
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 캐시 미스
		}
		return nil, fmt.Errorf("캐시 조회 실패: %w", err)
	}

	var resp model.ChatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("캐시 역직렬화 실패: %w", err)
	}

	return &resp, nil
}

// Set - 응답을 캐시에 저장
func (c *Cache) Set(ctx context.Context, req *model.ChatRequest, resp *model.ChatResponse) error {
	key := c.buildKey(req)
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("캐시 직렬화 실패: %w", err)
	}

	return c.rdb.Set(ctx, key, data, c.ttl).Err()
}

// buildKey - 요청 해시 기반 캐시 키 생성
// provider + model + messages 내용을 해시하여 키를 만든다.
func (c *Cache) buildKey(req *model.ChatRequest) string {
	h := sha256.New()
	h.Write([]byte(req.Provider))
	h.Write([]byte(req.Model))
	for _, msg := range req.Messages {
		h.Write([]byte(msg.Role))
		h.Write([]byte(msg.Content))
	}
	// 옵션도 키에 포함 (같은 메시지라도 temperature가 다르면 다른 응답)
	if req.Options.Temperature != nil {
		h.Write([]byte(fmt.Sprintf("t:%f", *req.Options.Temperature)))
	}
	if req.Options.MaxTokens != nil {
		h.Write([]byte(fmt.Sprintf("m:%d", *req.Options.MaxTokens)))
	}
	hash := hex.EncodeToString(h.Sum(nil))[:32]
	return cacheKeyPrefix + hash
}
