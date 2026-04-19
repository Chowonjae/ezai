package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// RequestLog - request_logs 테이블에 기록할 데이터
type RequestLog struct {
	TraceID           string
	ClientID          string
	ClientIP          string
	APIKeyID          int64 // 사용된 API 키 ID (FK)
	Project           string
	Task              string
	RequestedProvider string
	RequestedModel    string
	PromptHash        string
	InputPreview      string
	OptionsJSON       string
	ActualProvider    string
	ActualModel       string
	FallbackUsed      bool
	FallbackChain     []FallbackAttemptLog
	RoutingReason     string
	Status            string // success, error, timeout, rate_limited
	ErrorCode         string
	ErrorMessage      string
	InputTokens       int
	OutputTokens      int
	CostUSD           float64
	LatencyMs         int64
	ProviderLatencyMs int64
	SearchGrounding   bool
	OutputPreview     string
	Metadata          map[string]any
}

// FallbackAttemptLog - fallback 시도 이력
type FallbackAttemptLog struct {
	Order    int    `json:"order"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	LatencyMs int64 `json:"latency_ms"`
}

// RequestLogWriter - 비동기 요청 로그 기록기
// 버퍼 채널을 통해 로그를 비동기로 기록하여 응답 지연을 방지한다.
type RequestLogWriter struct {
	ch     chan *RequestLog
	db     *sql.DB
	logger *zap.Logger
	done   chan struct{}
}

// NewRequestLogWriter - 비동기 로그 기록기 생성
// bufSize: 버퍼 채널 크기 (대기열)
func NewRequestLogWriter(db *sql.DB, logger *zap.Logger, bufSize int) *RequestLogWriter {
	if bufSize <= 0 {
		bufSize = 1000
	}
	w := &RequestLogWriter{
		ch:     make(chan *RequestLog, bufSize),
		db:     db,
		logger: logger,
		done:   make(chan struct{}),
	}
	go w.consume()
	return w
}

// Write - 로그를 비동기 큐에 추가
// 채널이 가득 차면 드롭하고 경고 로그를 남긴다 (응답 지연 방지).
func (w *RequestLogWriter) Write(log *RequestLog) {
	select {
	case w.ch <- log:
	default:
		w.logger.Warn("요청 로그 버퍼 초과, 로그 드롭",
			zap.String("trace_id", log.TraceID),
		)
	}
}

// Close - 기록기 종료 (남은 로그 처리 후 종료)
func (w *RequestLogWriter) Close() {
	close(w.ch)
	<-w.done
}

// consume - 채널에서 로그를 꺼내 DB에 기록
func (w *RequestLogWriter) consume() {
	defer close(w.done)
	for log := range w.ch {
		if err := w.insert(log); err != nil {
			w.logger.Error("요청 로그 기록 실패",
				zap.String("trace_id", log.TraceID),
				zap.Error(err),
			)
		}
	}
}

// insert - 단건 로그 INSERT
func (w *RequestLogWriter) insert(log *RequestLog) error {
	// fallback 체인 JSON 직렬화
	var fallbackJSON *string
	if len(log.FallbackChain) > 0 {
		if data, err := json.Marshal(log.FallbackChain); err == nil {
			s := string(data)
			fallbackJSON = &s
		}
	}

	// 메타데이터 JSON 직렬화
	var metadataJSON *string
	if len(log.Metadata) > 0 {
		if data, err := json.Marshal(log.Metadata); err == nil {
			s := string(data)
			metadataJSON = &s
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	fallbackUsed := 0
	if log.FallbackUsed {
		fallbackUsed = 1
	}
	searchGrounding := 0
	if log.SearchGrounding {
		searchGrounding = 1
	}

	var apiKeyID *int64
	if log.APIKeyID > 0 {
		apiKeyID = &log.APIKeyID
	}

	_, err := w.db.Exec(`
		INSERT INTO request_logs (
			trace_id, timestamp, client_id, client_ip, api_key_id, project, task,
			requested_provider, requested_model, prompt_hash, input_preview, options_json,
			actual_provider, actual_model, fallback_used, fallback_chain_json, routing_reason,
			status, error_code, error_message, input_tokens, output_tokens,
			cost_usd, latency_ms, provider_latency_ms, search_grounding,
			output_preview, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.TraceID, now, log.ClientID, log.ClientIP, apiKeyID, log.Project, log.Task,
		log.RequestedProvider, log.RequestedModel, log.PromptHash, log.InputPreview, log.OptionsJSON,
		log.ActualProvider, log.ActualModel, fallbackUsed, fallbackJSON, log.RoutingReason,
		log.Status, nilIfEmpty(log.ErrorCode), nilIfEmpty(log.ErrorMessage),
		log.InputTokens, log.OutputTokens,
		log.CostUSD, log.LatencyMs, log.ProviderLatencyMs, searchGrounding,
		log.OutputPreview, metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("로그 INSERT 실패: %w", err)
	}
	return nil
}

// LogEntry - 로그 조회 결과 행
type LogEntry struct {
	ID                int64   `json:"id"`
	TraceID           string  `json:"trace_id"`
	Timestamp         string  `json:"timestamp"`
	ClientID          *string `json:"client_id"`
	ClientIP          *string `json:"client_ip"`
	Project           *string `json:"project"`
	RequestedProvider *string `json:"requested_provider"`
	RequestedModel    *string `json:"requested_model"`
	ActualProvider    *string `json:"actual_provider"`
	ActualModel       *string `json:"actual_model"`
	FallbackUsed      bool    `json:"fallback_used"`
	Status            string  `json:"status"`
	ErrorMessage      *string `json:"error_message,omitempty"`
	InputTokens       *int    `json:"input_tokens"`
	OutputTokens      *int    `json:"output_tokens"`
	CostUSD           *float64 `json:"cost_usd"`
	LatencyMs         *int64  `json:"latency_ms"`
	RoutingReason     *string `json:"routing_reason,omitempty"`
}

// LogQuery - 로그 조회 조건
type LogQuery struct {
	TraceID      string
	ClientID     string
	Provider     string
	Project      string
	Status       string
	FallbackUsed *bool
	Date         string
	From         string
	To           string
	Limit        int
}

// RequestLogReader - 로그 조회기
type RequestLogReader struct {
	db *sql.DB
}

// NewRequestLogReader - 로그 조회기 생성
func NewRequestLogReader(db *sql.DB) *RequestLogReader {
	return &RequestLogReader{db: db}
}

// Query - 로그 조회
func (r *RequestLogReader) Query(q LogQuery) ([]LogEntry, error) {
	var conditions []string
	var args []any

	if q.TraceID != "" {
		conditions = append(conditions, "trace_id = ?")
		args = append(args, q.TraceID)
	}
	if q.ClientID != "" {
		conditions = append(conditions, "client_id = ?")
		args = append(args, q.ClientID)
	}
	if q.Provider != "" {
		conditions = append(conditions, "(actual_provider = ? OR requested_provider = ?)")
		args = append(args, q.Provider, q.Provider)
	}
	if q.Project != "" {
		conditions = append(conditions, "project = ?")
		args = append(args, q.Project)
	}
	if q.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, q.Status)
	}
	if q.FallbackUsed != nil {
		val := 0
		if *q.FallbackUsed {
			val = 1
		}
		conditions = append(conditions, "fallback_used = ?")
		args = append(args, val)
	}
	if q.Date != "" {
		conditions = append(conditions, "date(timestamp) = ?")
		args = append(args, q.Date)
	}
	if q.From != "" && q.To != "" {
		conditions = append(conditions, "date(timestamp) BETWEEN ? AND ?")
		args = append(args, q.From, q.To)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinStrings(conditions, " AND ")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT id, trace_id, timestamp, client_id, client_ip, project,
		       requested_provider, requested_model, actual_provider, actual_model,
		       fallback_used, status, error_message, input_tokens, output_tokens,
		       cost_usd, latency_ms, routing_reason
		FROM request_logs %s
		ORDER BY id DESC LIMIT %d`, where, limit)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("로그 조회 실패: %w", err)
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var fallback int
		if err := rows.Scan(
			&e.ID, &e.TraceID, &e.Timestamp, &e.ClientID, &e.ClientIP, &e.Project,
			&e.RequestedProvider, &e.RequestedModel, &e.ActualProvider, &e.ActualModel,
			&fallback, &e.Status, &e.ErrorMessage, &e.InputTokens, &e.OutputTokens,
			&e.CostUSD, &e.LatencyMs, &e.RoutingReason,
		); err != nil {
			return nil, err
		}
		e.FallbackUsed = fallback == 1
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// LogStatsQuery - 로그 통계 조회 조건
type LogStatsQuery struct {
	GroupBy string // actual_provider, actual_model, project, client_id, status
	Date    string
	From    string
	To      string
}

// LogStatsEntry - 로그 통계 항목
type LogStatsEntry struct {
	GroupKey     string  `json:"group_key"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// Stats - GET /admin/logs/stats 로그 통계 집계
func (r *RequestLogReader) Stats(q LogStatsQuery) ([]LogStatsEntry, error) {
	groupCol := mapStatsGroupBy(q.GroupBy)
	if groupCol == "" {
		return nil, fmt.Errorf("유효하지 않은 group_by: %s", q.GroupBy)
	}

	var conditions []string
	var args []any

	if q.Date != "" {
		conditions = append(conditions, "date(timestamp) = ?")
		args = append(args, q.Date)
	}
	if q.From != "" && q.To != "" {
		conditions = append(conditions, "date(timestamp) BETWEEN ? AND ?")
		args = append(args, q.From, q.To)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinStrings(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT %s, COUNT(*),
		       SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),
		       COALESCE(AVG(latency_ms), 0),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0)
		FROM request_logs %s
		GROUP BY %s ORDER BY COUNT(*) DESC`,
		groupCol, whereClause, groupCol)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("통계 조회 실패: %w", err)
	}
	defer rows.Close()

	var entries []LogStatsEntry
	for rows.Next() {
		var e LogStatsEntry
		if err := rows.Scan(
			&e.GroupKey, &e.Requests, &e.Errors, &e.AvgLatencyMs,
			&e.InputTokens, &e.OutputTokens, &e.CostUSD,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// mapStatsGroupBy - group_by 파라미터를 DB 컬럼명으로 매핑
func mapStatsGroupBy(groupBy string) string {
	switch groupBy {
	case "provider", "actual_provider":
		return "actual_provider"
	case "model", "actual_model":
		return "actual_model"
	case "project":
		return "project"
	case "client_id":
		return "client_id"
	case "status":
		return "status"
	default:
		return ""
	}
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
