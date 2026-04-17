package crypto

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("키 생성 실패: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("암호화기 생성 실패: %v", err)
	}

	original := "sk-ant-api03-test-key-12345"
	encrypted, err := enc.Encrypt([]byte(original))
	if err != nil {
		t.Fatalf("암호화 실패: %v", err)
	}

	// 암호문은 원문과 달라야 함
	if string(encrypted) == original {
		t.Error("암호문이 원문과 동일함")
	}

	decrypted, err := enc.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("복호화 실패: %v", err)
	}

	if string(decrypted) != original {
		t.Errorf("복호화 결과 불일치: got %s, want %s", string(decrypted), original)
	}
}

func TestEncryptDecryptJSON(t *testing.T) {
	key, _ := GenerateKey()
	enc, _ := NewEncryptor(key)

	// 서비스 계정 JSON과 유사한 큰 데이터
	original := `{"type":"service_account","project_id":"test-project","private_key":"-----BEGIN PRIVATE KEY-----\nMIIEvQ...\n-----END PRIVATE KEY-----\n"}`

	encrypted, err := enc.Encrypt([]byte(original))
	if err != nil {
		t.Fatalf("JSON 암호화 실패: %v", err)
	}

	decrypted, err := enc.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("JSON 복호화 실패: %v", err)
	}

	if string(decrypted) != original {
		t.Error("JSON 복호화 결과 불일치")
	}
}

func TestInvalidKey(t *testing.T) {
	// 짧은 키
	_, err := NewEncryptor("abcd")
	if err == nil {
		t.Error("짧은 키에서 에러가 발생하지 않음")
	}

	// 잘못된 hex
	_, err = NewEncryptor("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Error("잘못된 hex에서 에러가 발생하지 않음")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()
	enc1, _ := NewEncryptor(key1)
	enc2, _ := NewEncryptor(key2)

	encrypted, _ := enc1.Encrypt([]byte("secret"))
	_, err := enc2.Decrypt(encrypted)
	if err == nil {
		t.Error("다른 키로 복호화 시 에러가 발생하지 않음")
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("키 생성 실패: %v", err)
	}
	if len(key) != 64 {
		t.Errorf("키 길이: got %d, want 64", len(key))
	}

	// 두 번 생성하면 다른 키
	key2, _ := GenerateKey()
	if key == key2 {
		t.Error("동일한 키가 두 번 생성됨")
	}
}
