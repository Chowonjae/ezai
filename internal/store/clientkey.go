package store

import (
	"database/sql"
	"fmt"
	"time"
)

// ClientKey - client_keys 테이블 행
type ClientKey struct {
	ID           int64  `json:"id"`
	ClientID     string `json:"client_id"`
	SecretHash   string `json:"-"` // JSON 응답에 절대 포함하지 않음
	SecretPrefix string `json:"secret_prefix"`
	ServiceName  string `json:"service_name"`
	Description  string `json:"description"`
	IsActive     bool   `json:"is_active"`
	ExpiresAt    string `json:"expires_at"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// ClientKeyStore - 클라이언트 키 저장소
type ClientKeyStore struct {
	db *sql.DB
}

// NewClientKeyStore - 클라이언트 키 저장소 생성
func NewClientKeyStore(db *sql.DB) *ClientKeyStore {
	return &ClientKeyStore{db: db}
}

// Create - 클라이언트 키 등록
func (s *ClientKeyStore) Create(clientID, secretHash, secretPrefix, serviceName, description string, expiresAt time.Time) (*ClientKey, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	exp := expiresAt.UTC().Format(time.RFC3339)

	result, err := s.db.Exec(
		`INSERT INTO client_keys (client_id, secret_hash, secret_prefix, service_name, description, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		clientID, secretHash, secretPrefix, serviceName, description, exp, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("클라이언트 키 등록 실패: %w", err)
	}

	id, _ := result.LastInsertId()
	return &ClientKey{
		ID: id, ClientID: clientID, SecretPrefix: secretPrefix,
		ServiceName: serviceName, Description: description,
		IsActive: true, ExpiresAt: exp, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetByClientID - client_id로 키 조회 (secret_hash 포함)
func (s *ClientKeyStore) GetByClientID(clientID string) (*ClientKey, error) {
	var k ClientKey
	err := s.db.QueryRow(
		`SELECT id, client_id, secret_hash, secret_prefix, service_name, description, is_active, expires_at, created_at, updated_at
		 FROM client_keys WHERE client_id = ?`, clientID,
	).Scan(&k.ID, &k.ClientID, &k.SecretHash, &k.SecretPrefix, &k.ServiceName, &k.Description, &k.IsActive, &k.ExpiresAt, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("클라이언트 키 조회 실패: %w", err)
	}
	return &k, nil
}

// List - 전체 키 목록 (secret_hash 제외)
func (s *ClientKeyStore) List() ([]ClientKey, error) {
	rows, err := s.db.Query(
		`SELECT id, client_id, secret_prefix, service_name, description, is_active, expires_at, created_at, updated_at
		 FROM client_keys ORDER BY service_name, client_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("클라이언트 키 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	var keys []ClientKey
	for rows.Next() {
		var k ClientKey
		if err := rows.Scan(&k.ID, &k.ClientID, &k.SecretPrefix, &k.ServiceName, &k.Description, &k.IsActive, &k.ExpiresAt, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ListByService - 서비스별 키 목록
func (s *ClientKeyStore) ListByService(serviceName string) ([]ClientKey, error) {
	rows, err := s.db.Query(
		`SELECT id, client_id, secret_prefix, service_name, description, is_active, expires_at, created_at, updated_at
		 FROM client_keys WHERE service_name = ? ORDER BY client_id`, serviceName,
	)
	if err != nil {
		return nil, fmt.Errorf("서비스별 키 조회 실패: %w", err)
	}
	defer rows.Close()

	var keys []ClientKey
	for rows.Next() {
		var k ClientKey
		if err := rows.Scan(&k.ID, &k.ClientID, &k.SecretPrefix, &k.ServiceName, &k.Description, &k.IsActive, &k.ExpiresAt, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// RotateSecret - 시크릿만 교체 (client_id, expires_at 유지)
func (s *ClientKeyStore) RotateSecret(clientID, newSecretHash, newSecretPrefix string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE client_keys SET secret_hash = ?, secret_prefix = ?, updated_at = ?
		 WHERE client_id = ? AND is_active = 1`,
		newSecretHash, newSecretPrefix, now, clientID,
	)
	if err != nil {
		return fmt.Errorf("시크릿 교체 실패: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("활성 키를 찾을 수 없습니다: %s", clientID)
	}
	return nil
}

// Deactivate - 키 비활성화
func (s *ClientKeyStore) Deactivate(clientID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE client_keys SET is_active = 0, updated_at = ? WHERE client_id = ?`,
		now, clientID,
	)
	if err != nil {
		return fmt.Errorf("키 비활성화 실패: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("키를 찾을 수 없습니다: %s", clientID)
	}
	return nil
}

// Reissue - 만료/비활성 키 재발급 (새 시크릿 + 새 만료일)
func (s *ClientKeyStore) Reissue(clientID, newSecretHash, newSecretPrefix string, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	exp := expiresAt.UTC().Format(time.RFC3339)
	result, err := s.db.Exec(
		`UPDATE client_keys SET secret_hash = ?, secret_prefix = ?, is_active = 1, expires_at = ?, updated_at = ?
		 WHERE client_id = ?`,
		newSecretHash, newSecretPrefix, exp, now, clientID,
	)
	if err != nil {
		return fmt.Errorf("키 재발급 실패: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("키를 찾을 수 없습니다: %s", clientID)
	}
	return nil
}
