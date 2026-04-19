package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Encryptor - AES-256-GCM 암호화/복호화
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor - 암호화기 생성
// key는 32바이트(256비트) hex 문자열 (64자)
func NewEncryptor(hexKey string) (*Encryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("암호화 키 디코딩 실패: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("암호화 키는 32바이트(hex 64자)여야 합니다 (현재: %d바이트)", len(key))
	}

	block, err := aes.NewCipher(key)
	// 키 바이트를 메모리에서 즉시 삭제 (힙 덤프/코어 덤프 시 노출 방지)
	for i := range key {
		key[i] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("AES 블록 생성 실패: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("GCM 생성 실패: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt - 평문을 AES-256-GCM으로 암호화
// 반환값: nonce + ciphertext (바이트 슬라이스)
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("논스 생성 실패: %w", err)
	}
	return e.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt - AES-256-GCM 암호문을 복호화
// 입력: nonce + ciphertext (Encrypt의 반환값)
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("암호문이 너무 짧습니다")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("복호화 실패: %w", err)
	}
	return plaintext, nil
}

// GenerateKey - 랜덤 256비트 키 생성 (hex 문자열 반환)
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("키 생성 실패: %w", err)
	}
	return hex.EncodeToString(key), nil
}
