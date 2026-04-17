package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Chowonjae/ezai/internal/crypto"
	"github.com/Chowonjae/ezai/internal/service"
	"github.com/Chowonjae/ezai/internal/store"
)

// ClientKeyAdminHandler - 클라이언트 키 관리 핸들러 (관리자 전용)
type ClientKeyAdminHandler struct {
	store     *store.ClientKeyStore
	auditLog  *store.AuditLog
	validator *service.ClientKeyService
	logger    *zap.Logger
}

// NewClientKeyAdminHandler - 클라이언트 키 관리 핸들러 생성
func NewClientKeyAdminHandler(store *store.ClientKeyStore, auditLog *store.AuditLog, validator *service.ClientKeyService, logger *zap.Logger) *ClientKeyAdminHandler {
	return &ClientKeyAdminHandler{
		store:     store,
		auditLog:  auditLog,
		validator: validator,
		logger:    logger,
	}
}

// createClientKeyRequest - 키 발급 요청
type createClientKeyRequest struct {
	ClientID    string `json:"client_id" binding:"required"`
	ServiceName string `json:"service_name" binding:"required"`
	Description string `json:"description"`
	TTLHours    int    `json:"ttl_hours" binding:"required,min=1"`
}

// Create - POST /admin/client-keys
// 새 클라이언트 키 쌍을 발급한다. 시크릿은 응답에서 1회만 노출된다.
func (h *ClientKeyAdminHandler) Create(c *gin.Context) {
	var req createClientKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	// 중복 확인
	existing, err := h.store.GetByClientID(req.ClientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "이미 존재하는 client_id입니다: " + req.ClientID})
		return
	}

	// 시크릿 생성
	secret, err := crypto.GenerateClientSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "시크릿 생성 실패"})
		return
	}

	secretHash := crypto.HashSecret(secret)
	secretPrefix := secret[:12] // "ezs_" + 8자
	expiresAt := time.Now().UTC().Add(time.Duration(req.TTLHours) * time.Hour)

	key, err := h.store.Create(req.ClientID, secretHash, secretPrefix, req.ServiceName, req.Description, expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 감사 로그
	_ = h.auditLog.Record(key.ID, "client_key_created", "admin", "클라이언트 키 발급: "+req.ClientID)

	h.logger.Info("클라이언트 키 발급",
		zap.String("client_id", req.ClientID),
		zap.String("service", req.ServiceName),
	)

	c.JSON(http.StatusCreated, gin.H{
		"client_key": key,
		"secret":     secret,
		"warning":    "이 시크릿은 다시 확인할 수 없습니다. 안전한 곳에 보관하세요.",
	})
}

// List - GET /admin/client-keys
func (h *ClientKeyAdminHandler) List(c *gin.Context) {
	var keys []store.ClientKey
	var err error

	if svc := c.Query("service"); svc != "" {
		keys, err = h.store.ListByService(svc)
	} else {
		keys, err = h.store.List()
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"client_keys": keys})
}

// Revoke - DELETE /admin/client-keys/:client_id
func (h *ClientKeyAdminHandler) Revoke(c *gin.Context) {
	clientID := c.Param("client_id")

	if err := h.store.Deactivate(clientID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 캐시 무효화
	if h.validator != nil {
		h.validator.InvalidateCache(clientID)
	}

	// 감사 로그 (ID 대신 0 사용, detail에 client_id 기록)
	_ = h.auditLog.Record(0, "client_key_revoked", "admin", "클라이언트 키 폐기: "+clientID)

	h.logger.Info("클라이언트 키 폐기", zap.String("client_id", clientID))

	c.JSON(http.StatusOK, gin.H{"message": "키가 비활성화되었습니다"})
}

// reissueRequest - 재발급 요청
type reissueRequest struct {
	TTLHours int `json:"ttl_hours" binding:"required,min=1"`
}

// Reissue - POST /admin/client-keys/:client_id/reissue
// 만료/비활성 키를 새 시크릿과 만료일로 재발급한다.
func (h *ClientKeyAdminHandler) Reissue(c *gin.Context) {
	clientID := c.Param("client_id")

	var req reissueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청: " + err.Error()})
		return
	}

	// 키 존재 확인
	existing, err := h.store.GetByClientID(clientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "키를 찾을 수 없습니다: " + clientID})
		return
	}

	// 새 시크릿 생성
	secret, err := crypto.GenerateClientSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "시크릿 생성 실패"})
		return
	}

	secretHash := crypto.HashSecret(secret)
	secretPrefix := secret[:12]
	expiresAt := time.Now().UTC().Add(time.Duration(req.TTLHours) * time.Hour)

	if err := h.store.Reissue(clientID, secretHash, secretPrefix, expiresAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 캐시 무효화
	if h.validator != nil {
		h.validator.InvalidateCache(clientID)
	}

	_ = h.auditLog.Record(existing.ID, "client_key_reissued", "admin", "클라이언트 키 재발급: "+clientID)

	h.logger.Info("클라이언트 키 재발급",
		zap.String("client_id", clientID),
	)

	c.JSON(http.StatusOK, gin.H{
		"client_id":  clientID,
		"secret":     secret,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"warning":    "이 시크릿은 다시 확인할 수 없습니다. 안전한 곳에 보관하세요.",
	})
}
