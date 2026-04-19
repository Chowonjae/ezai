package middleware

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TrustedNetworkOnly - 신뢰 네트워크에서만 접근 허용하는 미들웨어
// admin 라우트 등 외부 접근을 완전히 차단해야 하는 그룹에 사용한다.
func TrustedNetworkOnly(trustedCIDRs []string, logger *zap.Logger) gin.HandlerFunc {
	trustedNets := ParseCIDRs(trustedCIDRs, logger)

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP != nil {
			for _, trusted := range trustedNets {
				if trusted.Contains(clientIP) {
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "관리 API는 신뢰 네트워크에서만 접근할 수 있습니다",
		})
	}
}
