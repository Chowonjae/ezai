package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateLimitConfig - Rate Limiting 설정
type RateLimitConfig struct {
	RequestsPerMinute int // 클라이언트별 분당 요청 제한
}

// RateLimit - Redis 기반 Rate Limiting 미들웨어
// GCRA(Generic Cell Rate Algorithm) 알고리즘 사용
func RateLimit(rdb *redis.Client, cfg RateLimitConfig, logger *zap.Logger) gin.HandlerFunc {
	limiter := redis_rate.NewLimiter(rdb)

	rpm := cfg.RequestsPerMinute
	if rpm <= 0 {
		rpm = 60 // 기본: 분당 60회
	}
	limit := redis_rate.PerMinute(rpm)

	return func(c *gin.Context) {
		// client_id 기반 Rate Limiting
		clientID := GetClientID(c)
		if clientID == "" {
			clientID = c.ClientIP() // client_id 없으면 IP 기반
		}
		key := "ezai:ratelimit:" + clientID

		res, err := limiter.Allow(c.Request.Context(), key, limit)
		if err != nil {
			logger.Error("Rate Limiter 오류", zap.Error(err))
			c.Next() // Rate Limiter 장애 시 통과 (fail-open)
			return
		}

		// 응답 헤더에 Rate Limit 정보 추가
		c.Header("X-RateLimit-Limit", intToStr(rpm))
		c.Header("X-RateLimit-Remaining", intToStr(res.Remaining))

		if res.Allowed == 0 {
			retryAfter := res.RetryAfter.Seconds()
			c.Header("Retry-After", floatToStr(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "요청 제한 초과",
				"retry_after": retryAfter,
			})
			return
		}

		c.Next()
	}
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}

func floatToStr(f float64) string {
	return fmt.Sprintf("%.0f", f)
}
