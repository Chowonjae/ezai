package store

import (
	"database/sql"
	"fmt"
	"time"
)

// ArchiveResult - 아카이브 작업 결과
type ArchiveResult struct {
	ArchivedRows   int    `json:"archived_rows"`
	SummaryRows    int    `json:"summary_rows"`
	CutoffDate     string `json:"cutoff_date"`
	ArchivedAt     string `json:"archived_at"`
}

// SoftResetResult - 소프트 리셋 결과
type SoftResetResult struct {
	ResetAt string `json:"reset_at"`
	Message string `json:"message"`
}

// HardDeleteResult - 하드 삭제 결과
type HardDeleteResult struct {
	DeletedRows int    `json:"deleted_rows"`
	CutoffDate  string `json:"cutoff_date"`
	DeletedAt   string `json:"deleted_at"`
}

// UsageAdmin - 비용/로그 관리 (아카이브, 리셋, 삭제)
type UsageAdmin struct {
	db *sql.DB
}

// NewUsageAdmin - 비용 관리기 생성
func NewUsageAdmin(db *sql.DB) *UsageAdmin {
	return &UsageAdmin{db: db}
}

// Archive - 지정 날짜 이전 상세 로그를 일별 요약으로 집계 후 원본 삭제
// 1) request_logs에서 일별 집계 → archived_daily_summary에 INSERT
// 2) 집계 완료된 원본 로그 DELETE
func (ua *UsageAdmin) Archive(before string) (*ArchiveResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := ua.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	// 아카이브 대상 건수 확인
	var targetCount int
	err = tx.QueryRow(
		`SELECT COUNT(*) FROM request_logs WHERE date(timestamp) < ?`, before,
	).Scan(&targetCount)
	if err != nil {
		return nil, fmt.Errorf("아카이브 대상 조회 실패: %w", err)
	}
	if targetCount == 0 {
		return &ArchiveResult{
			ArchivedRows: 0,
			SummaryRows:  0,
			CutoffDate:   before,
			ArchivedAt:   now,
		}, nil
	}

	// 일별 집계 → archived_daily_summary에 INSERT
	insertSQL := `
		INSERT INTO archived_daily_summary
			(date, actual_provider, actual_model, project, client_id,
			 requests, input_tokens, output_tokens, cost_usd, avg_latency_ms,
			 error_count, archived_at)
		SELECT
			date(timestamp),
			actual_provider,
			actual_model,
			project,
			client_id,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(cost_usd), 0),
			COALESCE(AVG(latency_ms), 0),
			SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END),
			?
		FROM request_logs
		WHERE date(timestamp) < ?
		GROUP BY date(timestamp), actual_provider, actual_model, project, client_id`

	result, err := tx.Exec(insertSQL, now, before)
	if err != nil {
		return nil, fmt.Errorf("아카이브 집계 INSERT 실패: %w", err)
	}
	summaryRows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("RowsAffected 조회 실패: %w", err)
	}

	// 원본 로그 삭제
	delResult, err := tx.Exec(
		`DELETE FROM request_logs WHERE date(timestamp) < ?`, before,
	)
	if err != nil {
		return nil, fmt.Errorf("아카이브 원본 삭제 실패: %w", err)
	}
	deletedRows, err := delResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("RowsAffected 조회 실패: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("아카이브 트랜잭션 커밋 실패: %w", err)
	}

	return &ArchiveResult{
		ArchivedRows: int(deletedRows),
		SummaryRows:  int(summaryRows),
		CutoffDate:   before,
		ArchivedAt:   now,
	}, nil
}

// SoftReset - 집계 목적의 소프트 리셋
// request_logs의 비용 관련 필드를 0으로 초기화하되, 로그 자체는 보존한다.
// 주로 월별 예산 리셋 시 사용.
func (ua *UsageAdmin) SoftReset(before string) (*SoftResetResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := ua.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE request_logs
		SET cost_usd = 0
		WHERE date(timestamp) < ?`, before)
	if err != nil {
		return nil, fmt.Errorf("소프트 리셋 실패: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("소프트 리셋 커밋 실패: %w", err)
	}

	return &SoftResetResult{
		ResetAt: now,
		Message: fmt.Sprintf("%s 이전 로그의 비용이 리셋되었습니다", before),
	}, nil
}

// HardDelete - 지정 날짜 이전 로그 완전 삭제
// 개발/테스트 환경 정리 또는 GDPR 등 규정 준수 시 사용.
func (ua *UsageAdmin) HardDelete(before string) (*HardDeleteResult, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := ua.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`DELETE FROM request_logs WHERE date(timestamp) < ?`, before,
	)
	if err != nil {
		return nil, fmt.Errorf("하드 삭제 실패: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("RowsAffected 조회 실패: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("하드 삭제 커밋 실패: %w", err)
	}

	return &HardDeleteResult{
		DeletedRows: int(deleted),
		CutoffDate:  before,
		DeletedAt:   now,
	}, nil
}
