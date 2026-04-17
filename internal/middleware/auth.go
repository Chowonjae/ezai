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

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())

		// 신뢰 네트워크 확인
		if clientIP != nil {
			for _, trusted := range trustedNets {
				if trusted.Contains(clientIP) {
					// 신뢰 네트워크: X-Client-ID 헤더가 있으면 context에 설정
					if cid := c.GetHeader("X-Client-ID"); cid != "" {
						c.Set(ClientIDKey, cid)
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "인증 실패: " + err.Error(),
			})
			return
		}

		// context에 인증 정보 설정
		c.Set(ClientIDKey, validated.ClientID)
		c.Set("service_name", validated.ServiceName)

		// 응답 헤더에 만료 정보 추가
		c.Header("X-Key-Expires-At", validated.ExpiresAt.UTC().Format(time.RFC3339))
		remaining := time.Until(validated.ExpiresAt).Seconds()
		c.Header("X-Key-Expires-In", fmt.Sprintf("%.0f", remaining))

		c.Next()
	}
}
