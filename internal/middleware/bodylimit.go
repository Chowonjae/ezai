package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit - 요청 body 크기 제한 미들웨어
// maxBytes를 초과하는 요청은 413 Payload Too Large로 거부한다.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
