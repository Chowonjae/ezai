package store

import (
	"database/sql"
	"fmt"
	"time"
)

// AuditEntry - key_audit_log 테이블 행
type AuditEntry struct {
	ID            int64  `json:"id"`
	ProviderKeyID int64  `json:"provider_key_id"`
	Action        string `json:"action"`
	PerformedBy   string `json:"performed_by"`
	PerformedAt   string `json:"performed_at"`
	Detail        string `json:"detail"`
}

// AuditLog - 키 변경 감사 로그
type AuditLog struct {
	db *sql.DB
}

// NewAuditLog - 감사 로그 생성
func NewAuditLog(db *sql.DB) *AuditLog {
	return &AuditLog{db: db}
}

// Record - 감사 로그 기록
func (a *AuditLog) Record(keyID int64, action, performedBy, detail string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := a.db.Exec(
		`INSERT INTO key_audit_log (provider_key_id, action, performed_by, performed_at, detail)
		 VALUES (?, ?, ?, ?, ?)`,
		keyID, action, performedBy, now, detail,
	)
	if err != nil {
		return fmt.Errorf("감사 로그 기록 실패: %w", err)
	}
	return nil
}

// List - 감사 로그 목록 조회
func (a *AuditLog) List(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := a.db.Query(
		`SELECT id, provider_key_id, action, performed_by, performed_at, COALESCE(detail, '')
		 FROM key_audit_log ORDER BY performed_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("감사 로그 조회 실패: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ProviderKeyID, &e.Action, &e.PerformedBy, &e.PerformedAt, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// ListByKeyID - 특정 키의 감사 로그 조회
func (a *AuditLog) ListByKeyID(keyID int64) ([]AuditEntry, error) {
	rows, err := a.db.Query(
		`SELECT id, provider_key_id, action, performed_by, performed_at, COALESCE(detail, '')
		 FROM key_audit_log WHERE provider_key_id = ? ORDER BY performed_at DESC`, keyID,
	)
	if err != nil {
		return nil, fmt.Errorf("감사 로그 조회 실패: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ProviderKeyID, &e.Action, &e.PerformedBy, &e.PerformedAt, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}
