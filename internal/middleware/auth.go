package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ValidatedKey - 인증 성공 시 반환되는 키 정보
type ValidatedKey struct {
	ClientID    string
	ServiceName string
	ExpiresAt   time.Time
}

// ClientKeyValidator - 클라이언트 키 검증 인터페이스
type ClientKeyValidator interface {
	Validate(clientID, secret string) (*ValidatedKey, error)
}

// AuthConfig - 인증 미들웨어 설정
type AuthConfig struct {
	TrustedCIDRs []string           // 신뢰 네트워크 CIDR 목록
	Validator    ClientKeyValidator // 클라이언트 키 검증기 (nil이면 키 검증 비활성화)
}

// Auth - 네트워크 기반 인증 미들웨어
// 신뢰 네트워크(trusted CIDR)에서 온 요청은 인증 없이 통과하고,
// 외부 네트워크에서 온 요청은 X-Client-ID + X-Client-Secret 키 쌍 검증을 수행한다.
func Auth(cfg AuthConfig, logger *zap.Logger) gin.HandlerFunc {
	trustedNets := ParseCIDRs(cfg.TrustedCIDRs, logger)

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())

		// 신뢰 네트워크 확인
		if clientIP != nil {
			for _, trusted := range trustedNets {
				if trusted.Contains(clientIP) {
					// 신뢰 네트워크: X-Client-ID가 없으면 IP 기반 기본값 설정 (로그 귀속용)
					c.Set(TrustedNetKey, true)
					if cid := c.GetHeader("X-Client-ID"); cid != "" {
						c.Set(ClientIDKey, cid)
					} else {
						c.Set(ClientIDKey, "trusted-"+c.ClientIP())
					}
					c.Next()
					return
				}
			}
		}

		// 외부 네트워크: X-Client-ID + X-Client-Secret 검증
		clientID := c.GetHeader("X-Client-ID")
		clientSecret := c.GetHeader("X-Client-Secret")

		if clientID == "" || clientSecret == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "X-Client-ID와 X-Client-Secret 헤더가 필요합니다",
			})
			return
		}

		if cfg.Validator == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "클라이언트 키 검증이 설정되지 않았습니다",
			})
			return
		}

		validated, err := cfg.Validator.Validate(clientID, clientSecret)
		if err != nil {
			// 내부 에러 상세는 로그에만 기록 (계정 열거 공격 방지)
			logger.Warn("클라이언트 인증 실패",
				zap.String("client_id", clientID),
				zap.String("client_ip", c.ClientIP()),
				zap.Error(err),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "인증 실패",
			})
			return
		}

		// context에 인증 정보 설정
		c.Set(ClientIDKey, validated.ClientID)
		c.Set(ServiceNameKey, validated.ServiceName)

		// 응답 헤더에 만료 정보 추가
		c.Header("X-Key-Expires-At", validated.ExpiresAt.UTC().Format(time.RFC3339))
		remaining := time.Until(validated.ExpiresAt).Seconds()
		c.Header("X-Key-Expires-In", fmt.Sprintf("%.0f", remaining))

		c.Next()
	}
}
