package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Chowonjae/ezai/internal/crypto"
)

// ProviderKey - provider_keys 테이블 행
type ProviderKey struct {
	ID        int64  `json:"id"`
	Provider  string `json:"provider"`
	KeyName   string `json:"key_name"`
	KeyType   string `json:"key_type"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// 주의: 암호화된 값은 API 응답에 포함하지 않음
}

// KeyStore - API 키 저장소 (SQLCipher)
type KeyStore struct {
	db        *sql.DB
	encryptor *crypto.Encryptor
}

// NewKeyStore - 키 저장소 생성
func NewKeyStore(db *sql.DB, encryptor *crypto.Encryptor) *KeyStore {
	return &KeyStore{db: db, encryptor: encryptor}
}

// Create - API 키 등록 (동일 provider+key_name이 있으면 업데이트)
func (ks *KeyStore) Create(provider, keyName, keyValue, keyType string) (*ProviderKey, error) {
	// 키 값 암호화
	encrypted, err := ks.encryptor.Encrypt([]byte(keyValue))
	if err != nil {
		return nil, fmt.Errorf("키 암호화 실패: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 기존 키가 있으면 업데이트 + 재활성화, 없으면 신규 등록
	var existingID int64
	err = ks.db.QueryRow(
		`SELECT id FROM provider_keys WHERE provider = ? AND key_name = ?`,
		provider, keyName,
	).Scan(&existingID)

	if err == nil {
		// 기존 키 업데이트
		_, err = ks.db.Exec(
			`UPDATE provider_keys SET encrypted_value = ?, key_type = ?, is_active = 1, updated_at = ? WHERE id = ?`,
			encrypted, keyType, now, existingID,
		)
		if err != nil {
			return nil, fmt.Errorf("키 업데이트 실패: %w", err)
		}
		return &ProviderKey{
			ID: existingID, Provider: provider, KeyName: keyName,
			KeyType: keyType, IsActive: true, CreatedAt: now, UpdatedAt: now,
		}, nil
	}

	// 신규 등록
	result, err := ks.db.Exec(
		`INSERT INTO provider_keys (provider, key_name, encrypted_value, key_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		provider, keyName, encrypted, keyType, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("키 등록 실패: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("LastInsertId 조회 실패: %w", err)
	}
	return &ProviderKey{
		ID: id, Provider: provider, KeyName: keyName,
		KeyType: keyType, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Get - 키 ID로 조회 (복호화된 값 포함)
func (ks *KeyStore) Get(id int64) (*ProviderKey, string, error) {
	var key ProviderKey
	var encrypted []byte
	err := ks.db.QueryRow(
		`SELECT id, provider, key_name, encrypted_value, key_type, is_active, created_at, updated_at
		 FROM provider_keys WHERE id = ?`, id,
	).Scan(&key.ID, &key.Provider, &key.KeyName, &encrypted, &key.KeyType, &key.IsActive, &key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("키 조회 실패: %w", err)
	}

	decrypted, err := ks.encryptor.Decrypt(encrypted)
	if err != nil {
		return nil, "", fmt.Errorf("키 복호화 실패: %w", err)
	}

	return &key, string(decrypted), nil
}

// GetActiveByProvider - 프로바이더별 활성 키 조회 (복호화된 값 반환)
func (ks *KeyStore) GetActiveByProvider(provider string) (string, error) {
	var encrypted []byte
	err := ks.db.QueryRow(
		`SELECT encrypted_value FROM provider_keys
		 WHERE provider = ? AND is_active = 1
		 ORDER BY updated_at DESC LIMIT 1`, provider,
	).Scan(&encrypted)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("프로바이더 '%s'에 대한 활성 키가 없습니다", provider)
		}
		return "", fmt.Errorf("키 조회 실패: %w", err)
	}

	decrypted, err := ks.encryptor.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("키 복호화 실패: %w", err)
	}

	return string(decrypted), nil
}

// GetActiveByProviderWithID - 프로바이더별 활성 키 조회 (ID + 복호화된 값)
func (ks *KeyStore) GetActiveByProviderWithID(provider string) (int64, string, error) {
	var id int64
	var encrypted []byte
	err := ks.db.QueryRow(
		`SELECT id, encrypted_value FROM provider_keys
		 WHERE provider = ? AND is_active = 1
		 ORDER BY updated_at DESC LIMIT 1`, provider,
	).Scan(&id, &encrypted)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, "", fmt.Errorf("프로바이더 '%s'에 대한 활성 키가 없습니다", provider)
		}
		return 0, "", fmt.Errorf("키 조회 실패: %w", err)
	}

	decrypted, err := ks.encryptor.Decrypt(encrypted)
	if err != nil {
		return 0, "", fmt.Errorf("키 복호화 실패: %w", err)
	}

	return id, string(decrypted), nil
}

// List - 키 목록 조회 (값은 포함하지 않음)
func (ks *KeyStore) List() ([]ProviderKey, error) {
	rows, err := ks.db.Query(
		`SELECT id, provider, key_name, key_type, is_active, created_at, updated_at
		 FROM provider_keys ORDER BY provider, key_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("키 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	var keys []ProviderKey
	for rows.Next() {
		var k ProviderKey
		if err := rows.Scan(&k.ID, &k.Provider, &k.KeyName, &k.KeyType, &k.IsActive, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("키 목록 순회 실패: %w", err)
	}
	return keys, nil
}

// Update - 키 값 업데이트
func (ks *KeyStore) Update(id int64, newValue string) error {
	encrypted, err := ks.encryptor.Encrypt([]byte(newValue))
	if err != nil {
		return fmt.Errorf("키 암호화 실패: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = ks.db.Exec(
		`UPDATE provider_keys SET encrypted_value = ?, updated_at = ? WHERE id = ?`,
		encrypted, now, id,
	)
	return err
}

// Reactivate - 키 재활성화
func (ks *KeyStore) Reactivate(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := ks.db.Exec(
		`UPDATE provider_keys SET is_active = 1, updated_at = ? WHERE id = ?`,
		now, id,
	)
	return err
}

// Deactivate - 키 비활성화
func (ks *KeyStore) Deactivate(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := ks.db.Exec(
		`UPDATE provider_keys SET is_active = 0, updated_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("키를 찾을 수 없습니다 (id=%d)", id)
	}
	return nil
}

// Rotate - 키 로테이션 (기존 키 비활성화 + 새 키 등록)
// 트랜잭션으로 감싸서 비활성화와 등록이 원자적으로 수행되도록 한다.
func (ks *KeyStore) Rotate(id int64, newValue string) (*ProviderKey, error) {
	// 기존 키 조회
	key, _, err := ks.Get(id)
	if err != nil {
		return nil, err
	}

	// 새 키 값 암호화 (트랜잭션 밖에서 수행)
	encrypted, err := ks.encryptor.Encrypt([]byte(newValue))
	if err != nil {
		return nil, fmt.Errorf("키 암호화 실패: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 트랜잭션: 비활성화 + 등록을 원자적으로 수행
	tx, err := ks.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	// 기존 키 비활성화
	if _, err := tx.Exec(
		`UPDATE provider_keys SET is_active = 0, updated_at = ? WHERE id = ?`,
		now, id,
	); err != nil {
		return nil, fmt.Errorf("기존 키 비활성화 실패: %w", err)
	}

	// 새 키 등록
	result, err := tx.Exec(
		`INSERT INTO provider_keys (provider, key_name, encrypted_value, key_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		key.Provider, key.KeyName, encrypted, key.KeyType, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("새 키 등록 실패: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("트랜잭션 커밋 실패: %w", err)
	}

	newID, _ := result.LastInsertId()
	return &ProviderKey{
		ID: newID, Provider: key.Provider, KeyName: key.KeyName,
		KeyType: key.KeyType, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}, nil
}
