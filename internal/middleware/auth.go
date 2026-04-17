package middleware

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuthConfig - 인증 미들웨어 설정
type AuthConfig struct {
	TrustedCIDRs []string // 신뢰 네트워크 CIDR 목록
	APIKeyHeader string   // API 키 헤더명 (기본: X-API-Key)
	ValidateKey  func(key string) bool // API 키 검증 함수
}

// Auth - 네트워크 기반 인증 미들웨어
// 신뢰 네트워크(trusted CIDR)에서 온 요청은 인증 없이 통과하고,
// 외부 네트워크에서 온 요청은 API 키 검증을 수행한다.
func Auth(cfg AuthConfig, logger *zap.Logger) gin.HandlerFunc {
	// CIDR 파싱 (서버 시작 시 한 번만 수행)
	var trustedNets []*net.IPNet
	for _, cidr := range cfg.TrustedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Warn("잘못된 CIDR 형식, 무시됨", zap.String("cidr", cidr), zap.Error(err))
			continue
		}
		trustedNets = append(trustedNets, ipNet)
	}

	if cfg.APIKeyHeader == "" {
		cfg.APIKeyHeader = "X-API-Key"
	}

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())

		// 신뢰 네트워크 확인 (비트 연산, ~50ns)
		if clientIP != nil {
			for _, trusted := range trustedNets {
				if trusted.Contains(clientIP) {
					c.Next()
					return
				}
			}
		}

		// 외부 네트워크: API 키 검증
		apiKey := c.GetHeader(cfg.APIKeyHeader)
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "API 키가 필요합니다",
			})
			return
		}

		if cfg.ValidateKey != nil && !cfg.ValidateKey(apiKey) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "유효하지 않은 API 키입니다",
			})
			return
		}

		c.Next()
	}
}
