package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockValidator - 테스트용 ClientKeyValidator
type mockValidator struct {
	validClientID string
	validSecret   string
	expiresAt     time.Time
}

func (m *mockValidator) Validate(clientID, secret string) (*ValidatedKey, error) {
	if clientID == m.validClientID && secret == m.validSecret {
		return &ValidatedKey{
			ClientID:    clientID,
			ServiceName: "test-service",
			ExpiresAt:   m.expiresAt,
		}, nil
	}
	return nil, errInvalidCredentials
}

var errInvalidCredentials = &authError{"유효하지 않은 인증 정보"}

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

func TestAuthTrustedCIDR(t *testing.T) {
	logger := zap.NewNop()

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"127.0.0.0/8", "::1/128"},
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
		Validator:    &mockValidator{},
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

	expiresAt := time.Now().Add(24 * time.Hour)

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"10.0.0.0/8"},
		Validator: &mockValidator{
			validClientID: "test-client",
			validSecret:   "ezs_valid_secret",
			expiresAt:     expiresAt,
		},
	}, logger))
	r.GET("/test", func(c *gin.Context) {
		cid := GetClientID(c)
		c.String(200, cid)
	})

	// 외부 + 유효한 키 쌍 → 200
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("X-Client-ID", "test-client")
	req.Header.Set("X-Client-Secret", "ezs_valid_secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("외부 유효키 응답: got %d, want 200", w.Code)
	}
	if w.Body.String() != "test-client" {
		t.Errorf("client_id: got %q, want %q", w.Body.String(), "test-client")
	}

	// 만료 헤더 확인
	if w.Header().Get("X-Key-Expires-At") == "" {
		t.Error("X-Key-Expires-At 헤더가 없습니다")
	}
	if w.Header().Get("X-Key-Expires-In") == "" {
		t.Error("X-Key-Expires-In 헤더가 없습니다")
	}

	// 외부 + 잘못된 시크릿 → 401
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "203.0.113.1:12345"
	req2.Header.Set("X-Client-ID", "test-client")
	req2.Header.Set("X-Client-Secret", "wrong-secret")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("외부 잘못된키 응답: got %d, want 401", w2.Code)
	}
}

func TestAuthExternalMissingClientID(t *testing.T) {
	logger := zap.NewNop()

	r := gin.New()
	r.Use(Auth(AuthConfig{
		TrustedCIDRs: []string{"10.0.0.0/8"},
		Validator:    &mockValidator{},
	}, logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	// X-Client-Secret만 있고 X-Client-ID 없음 → 401
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("X-Client-Secret", "some-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("client_id 누락 응답: got %d, want 401", w.Code)
	}
}
