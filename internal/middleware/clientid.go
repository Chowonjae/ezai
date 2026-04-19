package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ClientIDKey - 컨텍스트에서 client_id를 꺼내기 위한 키
const ClientIDKey = "client_id"

// ClientID - client_id 필수 헤더 검증 미들웨어
// 모든 요청에 X-Client-ID 헤더를 요구한다.
// 인증과 별개로, 어떤 서비스가 호출했는지 로깅하기 위해 필요하다.
func ClientID() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.GetHeader("X-Client-ID")
		if clientID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "X-Client-ID 헤더가 필요합니다",
			})
			return
		}
		c.Set(ClientIDKey, clientID)
		c.Next()
	}
}

// GetClientID - 컨텍스트에서 client_id 조회
func GetClientID(c *gin.Context) string {
	if v, ok := c.Get(ClientIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
