package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/middleware"
	"github.com/Chowonjae/ezai/internal/model"
	"github.com/Chowonjae/ezai/internal/provider"
)

// StreamHandler - SSE 스트리밍 핸들러
type StreamHandler struct {
	registry *provider.Registry
	logger   *zap.Logger
}

// NewStreamHandler - 스트리밍 핸들러 생성
func NewStreamHandler(registry *provider.Registry, logger *zap.Logger) *StreamHandler {
	return &StreamHandler{
		registry: registry,
		logger:   logger,
	}
}

// Stream - POST /chat/stream
// SSE(Server-Sent Events)로 스트리밍 응답을 반환한다.
func (h *StreamHandler) Stream(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "잘못된 요청 형식: " + err.Error(),
		})
		return
	}

	traceID := middleware.GetTraceID(c)

	// 프로바이더 조회
	p, err := h.registry.Get(req.Provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("스트리밍 요청 수신",
		zap.String("trace_id", traceID),
		zap.String("provider", req.Provider),
		zap.String("model", req.Model),
	)

	// 스트리밍 시작
	ch, err := p.ChatStream(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "스트리밍 시작 실패: " + err.Error(),
		})
		return
	}

	// SSE 헤더 설정
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Trace-ID", traceID)

	// SSE 이벤트 전송
	c.Stream(func(w io.Writer) bool {
		chunk, ok := <-ch
		if !ok {
			// 채널 닫힘
			fmt.Fprintf(w, "data: [DONE]\n\n")
			c.Writer.Flush()
			return false
		}

		if chunk.Error != nil {
			// 에러 이벤트
			errData, _ := json.Marshal(gin.H{"error": *chunk.Error})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errData)
			c.Writer.Flush()
			return false
		}

		if chunk.Done {
			// 최종 이벤트 (사용량 포함)
			if chunk.Usage != nil {
				usageData, _ := json.Marshal(gin.H{"usage": chunk.Usage})
				fmt.Fprintf(w, "event: usage\ndata: %s\n\n", usageData)
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			c.Writer.Flush()
			return false
		}

		// 텍스트 청크 이벤트
		chunkData, _ := json.Marshal(gin.H{"content": chunk.Content})
		fmt.Fprintf(w, "data: %s\n\n", chunkData)
		c.Writer.Flush()
		return true
	})

	h.logger.Info("스트리밍 완료",
		zap.String("trace_id", traceID),
		zap.String("provider", req.Provider),
	)
}
