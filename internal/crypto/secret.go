package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
)

const clientSecretPrefix = "ezs_"

// GenerateClientSecret - 클라이언트 시크릿 생성
// 형식: "ezs_" + 32바이트 랜덤 hex (총 68자)
func GenerateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("시크릿 생성 실패: %w", err)
	}
	return clientSecretPrefix + hex.EncodeToString(b), nil
}

// HashSecret - 시크릿의 SHA-256 해시 (hex 문자열)
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// CompareSecretHash - 시크릿과 해시를 timing-safe 비교
func CompareSecretHash(secret, expectedHash string) bool {
	actualHash := HashSecret(secret)
	return subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) == 1
}
