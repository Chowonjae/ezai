package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// UsageResult - 사용량 집계 결과
type UsageResult struct {
	Period    string           `json:"period"`
	Total    UsageSummary     `json:"total"`
	Breakdown []UsageBreakdown `json:"breakdown,omitempty"`
}

// UsageSummary - 사용량 요약
type UsageSummary struct {
	Requests    int     `json:"requests"`
	InputTokens int    `json:"input_tokens"`
	OutputTokens int   `json:"output_tokens"`
	CostUSD     float64 `json:"cost_usd"`
}

// UsageBreakdown - 그룹별 사용량
type UsageBreakdown struct {
	GroupKey     string  `json:"group_key"`
	Requests    int     `json:"requests"`
	InputTokens int     `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	CostUSD     float64 `json:"cost_usd"`
}

// UsageQuery - 사용량 조회 조건
type UsageQuery struct {
	Period   string // daily, monthly, yearly, custom
	Date     string // daily: 2026-04-17
	Month    string // monthly: 2026-04
	Year     string // yearly: 2026
	From     string // custom: 시작일
	To       string // custom: 종료일
	Provider string // 필터
	Model    string // 필터
	Project  string // 필터
	ClientID string // 필터
	GroupBy  string // provider, model, project, client_id
}

// UsageReader - request_logs 기반 사용량 조회
type UsageReader struct {
	db *sql.DB
}

// NewUsageReader - 사용량 조회기 생성
func NewUsageReader(db *sql.DB) *UsageReader {
	return &UsageReader{db: db}
}

// DB - 내부 DB 커넥션 반환 (다른 Reader와 공유용)
func (ur *UsageReader) DB() *sql.DB {
	return ur.db
}

// Query - 사용량 집계 조회
func (ur *UsageReader) Query(q UsageQuery) (*UsageResult, error) {
	// 기간 조건 구성
	dateFilter, dateArgs, periodLabel := buildDateFilter(q)

	// WHERE 조건 구성
	var conditions []string
	var args []any

	if dateFilter != "" {
		conditions = append(conditions, dateFilter)
		args = append(args, dateArgs...)
	}
	if q.Provider != "" {
		conditions = append(conditions, "actual_provider = ?")
		args = append(args, q.Provider)
	}
	if q.Model != "" {
		conditions = append(conditions, "actual_model = ?")
		args = append(args, q.Model)
	}
	if q.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, q.Project)
	}
	if q.ClientID != "" {
		conditions = append(conditions, "client_id = ?")
		args = append(args, q.ClientID)
	}
	// 성공한 요청만 집계
	conditions = append(conditions, "status = 'success'")

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// 총계 조회
	totalQuery := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cost_usd), 0)
		FROM request_logs %s`, whereClause)

	var total UsageSummary
	if err := ur.db.QueryRow(totalQuery, args...).Scan(
		&total.Requests, &total.InputTokens, &total.OutputTokens, &total.CostUSD,
	); err != nil {
		return nil, fmt.Errorf("총계 조회 실패: %w", err)
	}

	result := &UsageResult{
		Period: periodLabel,
		Total:  total,
	}

	// group_by 조회
	if q.GroupBy != "" {
		groupCol := mapGroupBy(q.GroupBy)
		if groupCol != "" {
			groupQuery := fmt.Sprintf(`
				SELECT %s, COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cost_usd), 0)
				FROM request_logs %s
				GROUP BY %s ORDER BY SUM(cost_usd) DESC`,
				groupCol, whereClause, groupCol)

			rows, err := ur.db.Query(groupQuery, args...)
			if err != nil {
				return nil, fmt.Errorf("그룹 조회 실패: %w", err)
			}
			defer rows.Close()

			for rows.Next() {
				var b UsageBreakdown
				if err := rows.Scan(&b.GroupKey, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.CostUSD); err != nil {
					return nil, err
				}
				result.Breakdown = append(result.Breakdown, b)
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// buildDateFilter - 기간 필터 SQL + 파라미터 + 라벨
func buildDateFilter(q UsageQuery) (string, []any, string) {
	switch q.Period {
	case "daily":
		if q.Date != "" {
			return "date(timestamp) = ?", []any{q.Date}, q.Date
		}
	case "monthly":
		if q.Month != "" {
			return "strftime('%Y-%m', timestamp) = ?", []any{q.Month}, q.Month
		}
	case "yearly":
		if q.Year != "" {
			return "strftime('%Y', timestamp) = ?", []any{q.Year}, q.Year
		}
	case "custom":
		if q.From != "" && q.To != "" {
			return "date(timestamp) BETWEEN ? AND ?", []any{q.From, q.To},
				fmt.Sprintf("%s ~ %s", q.From, q.To)
		}
	}
	return "", nil, "all"
}

// mapGroupBy - group_by 파라미터를 DB 컬럼명으로 매핑
func mapGroupBy(groupBy string) string {
	switch groupBy {
	case "provider":
		return "actual_provider"
	case "model":
		return "actual_model"
	case "project":
		return "project"
	case "client_id":
		return "client_id"
	default:
		return ""
	}
}
