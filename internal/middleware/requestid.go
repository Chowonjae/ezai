package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TraceIDKey - 컨텍스트에서 trace_id를 꺼내기 위한 키
const TraceIDKey = "trace_id"

// RequestID - trace_id 생성 미들웨어
// 모든 요청에 고유 trace_id를 부여하고, 컨텍스트와 응답 헤더에 주입한다.
// 형식: tr_YYYYMMDD_HHMMSS_{uuid_short}
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now()
		short := uuid.New().String()[:8]
		traceID := fmt.Sprintf("tr_%s_%s", now.Format("20060102_150405"), short)

		c.Set(TraceIDKey, traceID)
		c.Header("X-Trace-ID", traceID)
		c.Next()
	}
}

// GetTraceID - 컨텍스트에서 trace_id 조회
func GetTraceID(c *gin.Context) string {
	if v, ok := c.Get(TraceIDKey); ok {
		return v.(string)
	}
	return ""
}
