package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Chowonjae/ezai/internal/config"
	"github.com/Chowonjae/ezai/internal/store"
)

// validateDateParam - YYYY-MM-DD 형식의 날짜 파라미터 검증
// 빈 문자열은 허용 (선택 파라미터)
func validateDateParam(date, paramName string) (string, error) {
	if date == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", &paramError{param: paramName, msg: "날짜 형식이 올바르지 않습니다 (YYYY-MM-DD)"}
	}
	return date, nil
}

type paramError struct {
	param string
	msg   string
}

func (e *paramError) Error() string {
	return e.param + ": " + e.msg
}

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
	// 날짜 파라미터 검증
	for _, p := range []struct{ value, name string }{
		{c.Query("date"), "date"},
		{c.Query("from"), "from"},
		{c.Query("to"), "to"},
	} {
		if _, err := validateDateParam(p.value, p.name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

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
		// group_by 검증 에러는 400, 나머지는 500
		if strings.Contains(err.Error(), "유효하지 않은 group_by") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "사용량 조회 실패: " + err.Error()})
		}
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
