package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/bcrypt"
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

// HashSecret - 시크릿을 bcrypt로 해싱
// bcrypt는 내부적으로 솔트를 포함하므로 레인보우 테이블 공격에 안전하다.
func HashSecret(secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("시크릿 해싱 실패: %w", err)
	}
	return string(hash), nil
}

// CompareSecretHash - 시크릿과 해시를 비교
// 레거시 SHA-256 해시(hex 64자)도 호환 지원한다.
func CompareSecretHash(secret, storedHash string) bool {
	// 레거시 SHA-256 해시 감지: hex 64자 형식
	if len(storedHash) == 64 && isHexString(storedHash) {
		legacyHash := sha256.Sum256([]byte(secret))
		legacyHex := hex.EncodeToString(legacyHash[:])
		return subtle.ConstantTimeCompare([]byte(legacyHex), []byte(storedHash)) == 1
	}
	// bcrypt 해시 비교
	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(secret)) == nil
}

// isHexString - 문자열이 hex 형식인지 확인
func isHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
