package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAuthTrustedCIDR(t *testing.T) {
	logger := zap.NewNop()

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"127.0.0.0/8", "::1/128"},
		APIKeyHeader: "X-API-Key",
	}, logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// 신뢰 네트워크 (127.0.0.1) → 통과
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("신뢰 네트워크 응답: got %d, want 200", w.Code)
	}
}

func TestAuthExternalWithoutKey(t *testing.T) {
	logger := zap.NewNop()

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"10.0.0.0/8"},
		APIKeyHeader: "X-API-Key",
	}, logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// 외부 네트워크 + 키 없음 → 401
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("외부 무키 응답: got %d, want 401", w.Code)
	}
}

func TestAuthExternalWithValidKey(t *testing.T) {
	logger := zap.NewNop()

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"10.0.0.0/8"},
		APIKeyHeader: "X-API-Key",
		ValidateKey:  func(key string) bool { return key == "valid-key" },
	}, logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// 외부 + 유효한 키 → 200
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("X-API-Key", "valid-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("외부 유효키 응답: got %d, want 200", w.Code)
	}

	// 외부 + 잘못된 키 → 401
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "203.0.113.1:12345"
	req2.Header.Set("X-API-Key", "wrong-key")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("외부 잘못된키 응답: got %d, want 401", w2.Code)
	}
}
