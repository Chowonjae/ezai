package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/store"
)

// AdminHandler - 관리 API 핸들러
type AdminHandler struct {
	keyStore  *store.KeyStore
	auditLog  *store.AuditLog
	logReader *store.RequestLogReader
	logger    *zap.Logger
}

// NewAdminHandler - 관리 핸들러 생성
func NewAdminHandler(keyStore *store.KeyStore, auditLog *store.AuditLog, logReader *store.RequestLogReader, logger *zap.Logger) *AdminHandler {
	return &AdminHandler{
		keyStore:  keyStore,
		auditLog:  auditLog,
		logReader: logReader,
		logger:    logger,
	}
}

// --- 키 관리 API ---

// ListKeys - GET /admin/keys
func (h *AdminHandler) ListKeys(c *gin.Context) {
	keys, err := h.keyStore.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// createKeyRequest - 키 등록 요청 구조체
type createKeyRequest struct {
	Provider string `json:"provider" binding:"required"`
	KeyName  string `json:"key_name" binding:"required"`
	KeyValue string `json:"key_value" binding:"required"`
	KeyType  string `json:"key_type"` // 기본값: api_key
}

// CreateKey - POST /admin/keys
func (h *AdminHandler) CreateKey(c *gin.Context) {
	var req createKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}
	if req.KeyType == "" {
		req.KeyType = "api_key"
	}

	key, err := h.keyStore.Create(req.Provider, req.KeyName, req.KeyValue, req.KeyType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 감사 로그 기록
	clientID := c.GetHeader("X-Client-ID")
	_ = h.auditLog.Record(key.ID, "created", clientID, "키 등록: "+req.KeyName)

	h.logger.Info("API 키 등록",
		zap.String("provider", req.Provider),
		zap.String("key_name", req.KeyName),
	)

	c.JSON(http.StatusCreated, key)
}

// UpdateKey - PUT /admin/keys/:id
func (h *AdminHandler) UpdateKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 ID"})
		return
	}

	var req struct {
		KeyValue string `json:"key_value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	if err := h.keyStore.Update(id, req.KeyValue); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	clientID := c.GetHeader("X-Client-ID")
	_ = h.auditLog.Record(id, "updated", clientID, "키 값 업데이트")

	c.JSON(http.StatusOK, gin.H{"message": "키가 업데이트되었습니다"})
}

// RotateKey - POST /admin/keys/:id/rotate
func (h *AdminHandler) RotateKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 ID"})
		return
	}

	var req struct {
		KeyValue string `json:"key_value" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	newKey, err := h.keyStore.Rotate(id, req.KeyValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	clientID := c.GetHeader("X-Client-ID")
	_ = h.auditLog.Record(id, "rotated", clientID, "기존 키 비활성화")
	_ = h.auditLog.Record(newKey.ID, "created", clientID, "로테이션으로 생성된 새 키")

	c.JSON(http.StatusOK, newKey)
}

// DeleteKey - DELETE /admin/keys/:id
func (h *AdminHandler) DeleteKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 ID"})
		return
	}

	if err := h.keyStore.Deactivate(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	clientID := c.GetHeader("X-Client-ID")
	_ = h.auditLog.Record(id, "deactivated", clientID, "키 비활성화")

	c.JSON(http.StatusOK, gin.H{"message": "키가 비활성화되었습니다"})
}

// --- 감사 로그 API ---

// ListAuditLogs - GET /admin/keys/audit
func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	limit := 100
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	entries, err := h.auditLog.List(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"audit_logs": entries})
}

// --- 로그 조회 API ---

// ListLogs - GET /admin/logs
func (h *AdminHandler) ListLogs(c *gin.Context) {
	if h.logReader == nil {
		c.JSON(http.StatusOK, gin.H{"logs": []any{}})
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	var fallbackUsed *bool
	if fb := c.Query("fallback_used"); fb != "" {
		val := fb == "true" || fb == "1"
		fallbackUsed = &val
	}

	q := store.LogQuery{
		TraceID:      c.Query("trace_id"),
		ClientID:     c.Query("client_id"),
		Provider:     c.Query("provider"),
		Project:      c.Query("project"),
		Status:       c.Query("status"),
		FallbackUsed: fallbackUsed,
		Date:         c.Query("date"),
		From:         c.Query("from"),
		To:           c.Query("to"),
		Limit:        limit,
	}

	entries, err := h.logReader.Query(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  entries,
		"count": len(entries),
	})
}

// LogStats - GET /admin/logs/stats
// 로그 통계를 group_by 기준으로 집계하여 반환한다.
func (h *AdminHandler) LogStats(c *gin.Context) {
	if h.logReader == nil {
		c.JSON(http.StatusOK, gin.H{"stats": []any{}})
		return
	}

	groupBy := c.Query("group_by")
	if groupBy == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_by 파라미터가 필요합니다"})
		return
	}

	q := store.LogStatsQuery{
		GroupBy: groupBy,
		Date:    c.Query("date"),
		From:    c.Query("from"),
		To:      c.Query("to"),
	}

	entries, err := h.logReader.Stats(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": entries,
		"count": len(entries),
	})
}
