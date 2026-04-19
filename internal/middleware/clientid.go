package middleware

import (
	"net"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 컨텍스트 키 상수
const (
	ClientIDKey    = "client_id"
	ServiceNameKey = "service_name"
	TrustedNetKey  = "trusted_net"
)

// GetClientID - 컨텍스트에서 client_id 조회
func GetClientID(c *gin.Context) string {
	if v, ok := c.Get(ClientIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// IsTrustedNet - 요청이 신뢰 네트워크에서 온 것인지 확인
func IsTrustedNet(c *gin.Context) bool {
	v, ok := c.Get(TrustedNetKey)
	return ok && v.(bool)
}

// ParseCIDRs - CIDR 문자열 목록을 []*net.IPNet으로 파싱 (공통 함수)
func ParseCIDRs(cidrs []string, logger *zap.Logger) []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Warn("잘못된 CIDR 형식, 무시됨", zap.String("cidr", cidr), zap.Error(err))
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}
