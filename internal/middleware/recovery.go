package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery - 패닉 복구 미들웨어
// 핸들러나 이후 미들웨어에서 패닉이 발생해도 서버가 죽지 않도록 복구한다.
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				traceID := GetTraceID(c)
				logger.Error("패닉 복구",
					zap.Any("error", err),
					zap.String("trace_id", traceID),
					zap.String("path", c.Request.URL.Path),
					zap.String("method", c.Request.Method),
					zap.ByteString("stack", debug.Stack()),
				)
				// 이미 응답 헤더가 전송된 경우(스트리밍 등) JSON 응답 불가 → 연결 종료
				if c.Writer.Written() {
					c.Abort()
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":    "Internal Server Error",
					"trace_id": traceID,
				})
			}
		}()
		c.Next()
	}
}
