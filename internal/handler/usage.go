package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/store"
)

// UsageHandler - 비용/사용량 조회 핸들러
type UsageHandler struct {
	usageReader    *store.UsageReader
	pricingManager *config.PricingManager
}

// NewUsageHandler - 사용량 핸들러 생성
func NewUsageHandler(usageReader *store.UsageReader, pricingManager *config.PricingManager) *UsageHandler {
	return &UsageHandler{
		usageReader:    usageReader,
		pricingManager: pricingManager,
	}
}

// Usage - GET /usage
// 기간별, 프로바이더별, 모델별 비용/사용량 집계를 반환한다.
func (h *UsageHandler) Usage(c *gin.Context) {
	q := store.UsageQuery{
		Period:   c.DefaultQuery("period", "daily"),
		Date:     c.Query("date"),
		Month:    c.Query("month"),
		Year:     c.Query("year"),
		From:     c.Query("from"),
		To:       c.Query("to"),
		Provider: c.Query("provider"),
		Model:    c.Query("model"),
		Project:  c.Query("project"),
		ClientID: c.Query("client_id"),
		GroupBy:  c.Query("group_by"),
	}

	result, err := h.usageReader.Query(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "사용량 조회 실패: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Pricing - GET /usage/pricing
// 전체 가격 테이블을 반환한다.
func (h *UsageHandler) Pricing(c *gin.Context) {
	if h.pricingManager == nil {
		c.JSON(http.StatusOK, gin.H{"pricing": map[string]any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pricing": h.pricingManager.AllPricing()})
}
